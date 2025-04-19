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
	// "strconv"
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
	IP     string
	Weight int
	Sessions int // 各webサーバのセッション数
}

type Cluster struct {
	Cluster_LB string `json:"Cluster_LB"`
	Webs map[string]string
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
		Addr: "114.51.4.7:6379",
		DB:   0,
	})
)	

const (
	tcpPort string = ":8001"
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

	// read json file
	value, err := ioutil.ReadAll(file)
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}

	var raw map[string]map[string]string
	if err := json.Unmarshal(value, &raw); err != nil {
		log.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	clusters := make(map[string]Cluster)
	for name, values := range raw {
		cluster := Cluster{
			Webs: make(map[string]string),
		}
		for k, v := range values {
			if k == "cluster_lb" {
				cluster.Cluster_LB = v
			} else if strings.HasPrefix(k, "web") {
				cluster.Webs[k] = v
			}
		}
		clusters[name] = cluster
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
	ownClusterLB = ip_addresses[1]

	// fmt.Println(totalLBs, ownClusterLB, clusters)
	fmt.Println(totalLBs, ownClusterLB)

	var id int
	for _, cluster := range clusters {
		if cluster.Cluster_LB == ownClusterLB {
			// 自クラスタの Web サーバを抽出
			for key, ip := range cluster.Webs {
				if strings.HasPrefix(key, "web") {
					ownWebServers = append(ownWebServers, ip)
				}
			}
			// fmt.Printf("自クラスタ (%s): Webサーバ: %v\n", name, ownWebServers)
		} else {
			// 他クラスタの LB を clusterLBs に追加
			// fmt.Printf("%v(%T) %v(%T)\n", name, name, cluster, cluster)
			clusterLBs = append(clusterLBs, LoadBalancer{
				ID:        id,
				Address:   cluster.Cluster_LB,
				IsHealthy: true,
				Data:      0,
				Weight:    0,
			})
			id++
		}
	}

	for _, web := range ownWebServers {
		webServers = append(webServers, webServer{
			IP: web,
			Weight: 0,
			Sessions: 0,
		})
	}

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