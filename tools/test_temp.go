// ソケット通信検証用
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"os/exec"
	"context"
	"log"
	"io/ioutil"
	"strings"
	"strconv"
	"time"
	"sync"

	"github.com/redis/go-redis/v9"
)

type LoadBalancer struct {
	ID int
	Address string
	IsHealthy bool // ヘルスチェック用
	Data int
	Weight int
}

type Cluster struct {
	Cluster_LB string `json:"Cluster_LB"`
}

var (
	clusterLBs []LoadBalancer
	queue int
	wg sync.WaitGroup
	mutex sync.RWMutex

	ctx        = context.Background()
	redisClient *redis.Client
	flushOnStartup = false
	totalLBs int
	ownClusterLB string
	isLeader bool

	rdb = redis.NewClient(&redis.Options{
		Addr: "114.51.4.7:6379",
		DB:   0,
	})
)

const (
	socketPort string = ":8001"
	sleepTime int     = 1
	kappa float64 = 0.2

	redisHost  = "114.51.4.7:6379"
	redisKey   = "ready:"
	leaderLB   = "114.51.4.2"
	syncChan   = "sync_start"
)

func init(){
	// open json file 
	file, err := os.Open("./json/config.json")
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	value, err := ioutil.ReadAll(file)
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}

	var clusters map[string]Cluster
	err = json.Unmarshal(value, &clusters)
	if err != nil {
		log.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	totalLBs = len(clusters)

	// execute "hostname -i"
	cmd := exec.Command("hostname", "-i")
	output, err := cmd.Output()
	if err != nil {
		fmt.Printf("Error executing command: %v\n", err)
		return
	}

	// split output results by spaces and assign to array
	ip_addresses := strings.Fields(string(output))
	// take own Cluster_LB IP address
	ownClusterLB := ip_addresses[1]

	fmt.Println(len(clusters)) // display all number of LB

	// match with IP address of each Cluster_LB
	var id int
	for i, cluster := range clusters {
		if ownClusterLB != cluster.Cluster_LB {
			// add the IP address of each Cluster_LB to the list
			fmt.Printf("%v(%T) %v(%T)\n", i, i, cluster, cluster)
			clusterLBs = append(clusterLBs, LoadBalancer{
				ID: id,
				Address: cluster.Cluster_LB, 
				IsHealthy: true, // temporary
				Data: 0,
				Weight: 0,
			})
			id++
		}
	}

	fmt.Println(ownClusterLB, clusterLBs) // for debug

	// できればここで時刻に応じてLBを実行(mainに移行)する処理を書きたい
	isLeader = ownClusterLB == leaderLB // リーダーLBだけ true にする
	waitForAllLBsAndSyncStart(ctx, rdb, ownClusterLB, totalLBs, isLeader, "lb_ready:")
}

func waitForAllLBsAndSyncStart(ctx context.Context, rdb *redis.Client, ownClusterLB string, totalLBs int, isLeader bool, redisKey string) {
	// redisKey := "lb_ready:"
	pubsubChannel := "sync_start"

	// 自分の準備完了を通知
	if err := rdb.Set(ctx, redisKey+ownClusterLB, "true", 0).Err(); err != nil {
		log.Fatalf("Redis SET failed: %v", err)
	}
	fmt.Println("LB Ready sent:", redisKey+ownClusterLB)

	// sync_start 購読（リーダーも含めて全LBが購読）
	sub := rdb.Subscribe(ctx, pubsubChannel)
	defer sub.Close()

	// 最初のメッセージ受信を準備
	_, err := sub.Receive(ctx)
	if err != nil {
		log.Fatalf("Failed to subscribe to %s: %v", pubsubChannel, err)
	}
	ch := sub.Channel()

	if isLeader {
		// リーダーの役割：準備が揃ったらsync_startを発信
		fmt.Println("Coordinator waiting for all LB readiness...")
		for {
			keys, err := rdb.Keys(ctx, redisKey+"*").Result()
			if err != nil {
				log.Printf("Redis KEYS error: %v", err)
				time.Sleep(1 * time.Second)
				continue
			}
			if len(keys) == totalLBs {
				fmt.Printf("%d/%d ready. Publishing sync_start...\n", len(keys), totalLBs)
				break
			}
			fmt.Printf("%d/%d ready\n", len(keys), totalLBs)
			time.Sleep(1 * time.Second)
		}

		// sync_start メッセージ送信
		err := rdb.Publish(ctx, pubsubChannel, "start").Err()
		if err != nil {
			log.Fatalf("Failed to publish sync_start: %v", err)
		}
	}

	// 全員 sync_start を待つ（リーダーも含む）
	fmt.Println("Waiting for sync_start signal...")
	for msg := range ch {
		if msg.Payload == "start" {
			fmt.Println("Received sync_start signal. Proceeding to main.")
			break
		}
	}
}

func main(){

	for {
		queue++
		fmt.Println(queue)
		time.Sleep(time.Duration(sleepTime) * time.Second)
	}
	// wg.Add(1)
	// go Socket_Server()

	// for i, lb := range clusterLBs {
	// 	wg.Add(1)
	// 	go Socket_Client(lb, i)
	// }

	// wg.Add(1)
	// go func(){
	// 	defer wg.Done()

	// 	for {
	// 		queue++
	// 		time.Sleep(time.Duration(sleepTime) * time.Second)
	// 	}
	// }()

	// wg.Wait()
}

func Socket_Server(){
	defer wg.Done()

	addr, err := net.ResolveUDPAddr("udp", socketPort)
	if err != nil {
		fmt.Printf("Failed to resolve address: %v\n", err)
		return
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		fmt.Printf("Failed to listen: %v\n", err)
		return
	}
	defer conn.Close()

	buffer := make([]byte, 1024)

	for {
		n, remoteAddr, _ := conn.ReadFromUDP(buffer)
		msg := string(buffer[:n])
		fmt.Printf("Received from %s: %s\n", remoteAddr, msg)
	}
}

func Socket_Client(lb LoadBalancer, i int){
	defer wg.Done()

	addr, err := net.ResolveUDPAddr("udp", lb.Address + socketPort)
	if err != nil {
		fmt.Printf("LB %d: Failed to resolve address: %v\n", lb.ID, err)
		return
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		fmt.Printf("LB %d: Failed to connect to server: %v\n", lb.ID, err)
		return
	}
	defer conn.Close()

	ticker := time.NewTicker(time.Duration(sleepTime) * time.Second)
	defer ticker.Stop()

	buffer := make([]byte, 1024)

	for range ticker.C {
		mutex.RLock()
		msg := strconv.Itoa(queue)
		mutex.RUnlock()

		_, err := conn.Write([]byte(msg))
		if err != nil {
			fmt.Printf("Failed to send to %s: %v\n", lb.Address, err)
			continue
		}

		data, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			fmt.Printf("No data: %v\n", err)
			continue
		}

		tempData := string(buffer[:data])
		temp_Data, err := strconv.Atoi(tempData)
		if err != nil {
			fmt.Println("failure get request", err)
			continue
		}
		clusterLBs[i].Data = temp_Data
		
		Calculate(clusterLBs[i].Data, i)
		fmt.Println("Socket_Client: ", lb.ID, lb.Address, msg)
	}
}

func Calculate(next_queue int, num int) {
	// calculate DC method
	fmt.Printf("Calculate: %d(ID), %d(data)\n", num, next_queue)

	if queue > next_queue {
		diff := queue - next_queue
		clusterLBs[num].Weight = int(math.Round(kappa * float64(diff)))
	} else {
		clusterLBs[num].Weight = 0
	}

	fmt.Printf("weight: %d\n", clusterLBs[num].Weight)
}