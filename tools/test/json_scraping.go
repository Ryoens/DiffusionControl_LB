// json -> cluster, webを抽出
package main

import (
	"context"
	"encoding/json"
	"fmt"
	// "flag"
	// "io"
	"io/ioutil"
	"log"
	// "math"
	// "math/rand"
	// "net"
	// "net/http"
	// "net/http/httputil"
	// "net/url"
	"os"
	"os/exec"
	"strings"
	"strconv"
	"sort"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	// "google.golang.org/grpc"
	// "google.golang.org/grpc/codes"
	// "google.golang.org/grpc/status"

	pb "custome_weightedRR/api"
)

type LoadBalancer struct {
	ID int
	Address string
	IsHealthy bool // ヘルスチェック用
	Data int
	Weight int
}

type webServer struct {
	ID     int
	IP     string
	Weight int
	Sessions int // 各webサーバのセッション数
}

type Cluster struct {
	Cluster_LB string `json:"Cluster_LB"`
	Webs map[string]string
}

type ClusterJSON struct {
	AdjacentList map[string]string `json:"adjacentList"`
	InternalList map[string]string `json:"internalList"`
}

type Server struct {
	pb.UnimplementedLoadBalancerServer
}

// 集計データに関して(Response, splitData)

var (
	clusterLBs []LoadBalancer
	webServers []webServer
	ownWebServers []string

	wg sync.WaitGroup
	mutex sync.RWMutex

	ctx        = context.Background()
	redisClient *redis.Client
	totalLBs int
	queue int
	ownClusterLB string
	isLeader bool
	flushOnStartup = false

	rdb = redis.NewClient(&redis.Options{
		Addr: "10.0.255.2:6379",
		DB:   0,
	})
)	

const (
	tcpPort string = ":8001"
	sleepTime int     = 1
	kappa float64 = 0.2

	redisHost  = "10.0.255.2:6379"
	redisKey   = "ready:"
	leaderLB   = "172.18.4.2"
	syncChan   = "sync_start"
)

func init(){
	// open json file 
	file, err := os.Open("./json/adjacentList.json")
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	// read json file
	value, err := ioutil.ReadAll(file)
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}

	clusters := make(map[string]ClusterJSON)
	err = json.Unmarshal(value, &clusters)
	if err != nil {
		log.Fatal(err)
	}
	totalLBs = len(clusters)

	// execute "hostname -i"
	cmd := exec.Command("hostname", "-i")
	output, err := cmd.Output()
	if err != nil {
		panic(err)
	}
	ipAddresses := strings.Fields(string(output))

	if strings.HasPrefix(ipAddresses[0], "172.") {
		ownClusterLB = ipAddresses[0]
	} else {
		ownClusterLB = ipAddresses[1]
	}
	fmt.Println("ownClusterLB:", ownClusterLB, ipAddresses)

	var id, idWeb int
	for _, cluster := range clusters {
		clusterLBIP := cluster.InternalList["cluster_lb"]
		if clusterLBIP == ownClusterLB {
			for k, v := range cluster.AdjacentList {
				fmt.Println(k, v, id)
				// 隣接リストの登録
				clusterLBs = append(clusterLBs, LoadBalancer{
					ID:        id,
					Address:   v,
					IsHealthy: true,
					Data:      0,
					Weight:    0,
				})
				id++
			}
			// 自クラスタの Web サーバ登録
			for k, v := range cluster.InternalList {
				if strings.HasPrefix(k, "web") {
					ownWebServers = append(ownWebServers, v)
				}
			}
		}
	}

	// 自クラスタWebサーバ構造体へ追加
	for _, ip := range ownWebServers {
		webServers = append(webServers, webServer{
			ID:       idWeb,
			IP:       ip,
			Weight:   0,
			Sessions: 0,
		})
		idWeb++
	}

	sort.Slice(clusterLBs, func(i, j int) bool {
		return getLastOctet(clusterLBs[i].Address) < getLastOctet(clusterLBs[j].Address)
	})
	sort.Slice(webServers, func(i, j int) bool {
		return getLastOctet(webServers[i].IP) < getLastOctet(webServers[j].IP)
	})

	for i := range clusterLBs {
		getLastOctet(clusterLBs[i].Address)
		clusterLBs[i].ID = i
	}
	for i := range webServers {
		lastOctet := getLastOctet(webServers[i].IP)
		webServers[i].ID = lastOctet % 10
	}

	// 確認表示
	fmt.Println("ClusterLBs:")
	for _, lb := range clusterLBs {
		fmt.Printf("- %s (ID: %d)\n", lb.Address, lb.ID)
	}

	fmt.Println("Own Web Servers:")
	for _, ws := range webServers {
		fmt.Println("- " + ws.IP)
	}

	fmt.Println(clusterLBs, ownWebServers, webServers)

	fmt.Println(clusterLBs, ownWebServers, webServers)
	isLeader = ownClusterLB == leaderLB // リーダーLBだけ true にする
	waitForAllLBsAndSyncStart(ctx, rdb, ownClusterLB, totalLBs, isLeader, "lb_ready:")
}

func main(){
	for {
		queue++
		fmt.Println(queue)
		time.Sleep(time.Duration(sleepTime) * time.Second)
	}
}

func getLastOctet(ip string) int {
    parts := strings.Split(ip, ".")
    if len(parts) != 4 {
        fmt.Println("Invalid IP address format:", ip)
        return -1
    }
    last, err := strconv.Atoi(parts[3])
    if err != nil {
        fmt.Println("Invalid IP address:", ip)
        return -1
    }
    return last
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