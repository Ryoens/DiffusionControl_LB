package main

import(
	"os"
	"os/exec"
	"fmt"
	"log"
	"time"
	"net"
	"sync"
	"context"
	"strings"
	"strconv"
	"io/ioutil"
	"encoding/json"
	"math"
	"math/rand"
	"net/url"
	"net/http"
	"net/http/httputil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/codes"

	pb "custome_weightedRR/api"
)

type Server struct {
	IP	string 
	Weight	int
	// 追加で各webサーバが持つセッション数を入れるかも
}

type Cluster struct {
	Cluster_LB string `json:"cluster_lb"`
	Web0      string `json:"web0"`
	Web1      string `json:"web1"`
	Web2      string `json:"web2"`
}

type server struct {
	pb.UnimplementedLoadBalancerServer
}

// グローバル変数の定義
var (
	proxyIPs = []Server{
		{"", 0}, 
		{"", 0},
		{"", 0},
	}
	randomIndex Server
	clusterLBs []string // 隣接リスト
	my_clusterLB string
	data []int
	queue int // 処理待ちTCPセッション数
	weight int 
)

const (
	// 固定値の定義
	tcp_port string = ":8001"
	dst_port string = ":80" // webサーバ用
	grpc_dest string = ":50051" // gRPCで使用
	sleep_time int = 1
	threshold int = 700
	kappa float64 = 0.07
)

func init(){
	// JSONファイルを開く
	file, err := os.Open("./json/config.json")
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	// JSONファイルの内容を読み込む
	value, err := ioutil.ReadAll(file)
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}

	var clusters map[string]Cluster
	err = json.Unmarshal(value, &clusters)
	if err != nil {
		log.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	// hostname -i を実行
	cmd := exec.Command("hostname", "-i")
	output, err := cmd.Output()
	if err != nil {
		fmt.Printf("Error executing command: %v\n", err)
		return
	}

	// 出力結果をスペースで分割し、配列に代入
	ip_addresses := strings.Fields(string(output))

	// 自身のCluster_LBのIPアドレスを抽出
	clusterLB := ip_addresses[1] // 最初のIPアドレスを取得
	
	// 各クラスタのLBのIPアドレスと照合
	for _, cluster := range clusters {
		if clusterLB != cluster.Cluster_LB {
			// 各クラスタLBのIPアドレスをリストに追加
			clusterLBs = append(clusterLBs, cluster.Cluster_LB)
		} else {
			// proxyIPsにサーバ情報を設定
			proxyIPs[0].IP = cluster.Web0
			proxyIPs[1].IP = cluster.Web1
			proxyIPs[2].IP = cluster.Web2
			my_clusterLB = cluster.Cluster_LB
		} 
	}
	data = make([]int, len(clusterLBs))

	// デバック用
	// Cluster2の場合: 10.0.3.10 10.0.3.11 10.0.3.12 114.51.4.4 [114.51.4.2 114.51.4.3]
	fmt.Println(proxyIPs[0].IP, proxyIPs[1].IP, proxyIPs[2].IP, my_clusterLB, clusterLBs)
}

func main(){
	var wg sync.WaitGroup

	wg.Add(1)
	go gRPC_Server(&wg)

	wg.Add(1)
	go gRPC_Client(&wg)
	
	// 後で関数化するかも
	s := http.Server{
		Addr:	tcp_port,
		Handler: http.HandlerFunc(lbHandler),
	}

	fmt.Printf("HTTP server is listening on %s...\n", tcp_port)
	if err := s.ListenAndServe(); err != nil {
		log.Fatal(err.Error())
	}

	wg.Wait()
}

// リクエストをweighted RRで処理
func lbHandler(w http.ResponseWriter, r *http.Request) {
	queue++ // 処理待ちセッション数をインクリメント

	if queue > threshold {
		// Calculate関数で計算した値を該当IPアドレスの重みとして指定
	} else {
		// ランダムシードを設定
		rand.Seed(time.Now().UnixNano())
		for i := range proxyIPs{
			proxyIPs[i].Weight = rand.Intn(10)+1
		}

		for _, server := range proxyIPs {
			fmt.Printf("IP: %s, Weight: %d\n", server.IP, server.Weight)
		}

		randomIndex = WeightedRoundRobin()
		fmt.Println("Selected IP:", randomIndex)
		fmt.Printf("---\n")
	}

	proxyURL := &url.URL {
		Scheme: "http",
		Host: randomIndex.IP + dst_port,
	}

	fmt.Println(proxyURL)

	// make reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(proxyURL)
	proxy.ServeHTTP(w, r)
}

// 重みづけラウンドロビン(バックエンドサーバへの振り分け) -> 後で通常のラウンドロビンに変更するかも
func WeightedRoundRobin() Server {
	totalWeight := 0
	for _, server := range proxyIPs {
		totalWeight += server.Weight
	}

	// 0からtotalWeight-1までの乱数を生成
	randomWeight := rand.Intn(totalWeight)

	fmt.Printf("totalWeight: %d, randomWeight: %d\n", totalWeight, randomWeight)
	// 重みでサーバーを選択
	for _, server := range proxyIPs {
		if randomWeight < server.Weight {
			return server
		}
		randomWeight -= server.Weight
	}

	// ここには到達しないはずだが、デフォルトで最初のサーバーを返す
	return proxyIPs[0]
}

// gRPCサーバ
func gRPC_Server(wg *sync.WaitGroup) {
	defer wg.Done()

	lis, err := net.Listen("tcp", grpc_dest)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterLoadBalancerServer(s, &server{})
	log.Printf("gRPC Server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

// 隣接LBへのヘルスチェック
func (s *server) GetBackendStatus(ctx context.Context, req *pb.BackendRequest) (*pb.BackendStatus, error) {
	fmt.Printf("Received health check request for server: %s\n", req.ServerName)
	return &pb.BackendStatus{IsHealthy: true}, nil
}

// 隣接LBへの制御情報の送信
func (s *server) ControlStream(stream pb.LoadBalancer_ControlStreamServer) error {
	rand.Seed(time.Now().UnixNano()) // ランダムシードを設定

	for {
		in, err := stream.Recv()
		if err != nil {
			log.Printf("Error receiving control message: %v", err)
			return err
		}
		// 制御情報をランダムな整数として生成
		randomControlValue := rand.Intn(100) // 0〜99のランダム整数
		log.Printf("Received control command: %s, sending random value: %d", in.Command, randomControlValue)

		// ランダムな制御情報をクライアントに送信
		if err := stream.Send(&pb.ControlResponse{Status: "ok", Info: fmt.Sprintf("%d", randomControlValue)}); err != nil {
			log.Printf("Error sending response: %v", err)
			return err
		}
	}
}

func healthCheck(client pb.LoadBalancerClient, adjacent_lb string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(sleep_time)*time.Second)
	defer cancel()

	req := &pb.BackendRequest{ServerName: "server-1"}
	res, err := client.GetBackendStatus(ctx, req)
	if err != nil || !res.IsHealthy {
		log.Printf("Server %s is not healthy, trying the next one...", adjacent_lb)
		return false
	}

	log.Printf("Server %s is healthy, starting control stream...", adjacent_lb)
	return true
}

// gRPCクライアント(変更後)
func gRPC_Client(wg *sync.WaitGroup) {
	defer wg.Done()

	for i, adjacent_lb := range clusterLBs {
		adjacent_lb = adjacent_lb + grpc_dest
		// ヘルスチェック
		conn, err := grpc.Dial(adjacent_lb, grpc.WithInsecure(), grpc.WithBlock())
		if err != nil {
			log.Printf("Cound not connect to server %s: %v", adjacent_lb, err)
			continue
		}
		defer conn.Close()

		client := pb.NewLoadBalancerClient(conn)

		if healthCheck(client, adjacent_lb) {
			fmt.Println("gogogogo")
			wg.Add(1)
			go handleControlStream(wg, client, adjacent_lb, i)
		} else {
			log.Printf("Load Balancer at %s is down", adjacent_lb)
		}
		time.Sleep(time.Duration(sleep_time) * time.Second)
	}
	wg.Wait()
}

func handleControlStream(wg *sync.WaitGroup, client pb.LoadBalancerClient, address string, num int) {
	defer wg.Done()

	fmt.Println("connection", client)

	// 双方向ストリーミングの制御情報送受信
	stream, err := client.ControlStream(context.Background())
	if err != nil {
		log.Fatalf("Error creating stream: %v", err)
		return
	}

	// 定期的にヘルスチェックと制御情報を送受信
	ticker := time.NewTicker(time.Duration(sleep_time) * time.Second)
	for range ticker.C {
		// 制御情報の送信
		if err := stream.Send(&pb.ControlMessage{Command: "update_policy", Payload: "new_policy_data"}); err != nil {
			log.Printf("Error sending control message: %v", err)

			if status.Code(err) == codes.Canceled || status.Code(err) == codes.Unavailable {
				log.Printf("Send Connection to %s was lost, reconnecting...", address)
				return
			}
			return
		}

		// 制御情報の応答受信
		in, err := stream.Recv()
		if err != nil {
			log.Printf("Error receiving control response: %v", err)

			if status.Code(err) == codes.Canceled || status.Code(err) == codes.Unavailable {
				log.Printf("Receive Connection to %s was lost, reconnecting...", address)
				return
			}
			return
		}
		log.Printf("Received control response: %s", in.Info)
		
		fmt.Println("convert")
		data[num], err = strconv.Atoi(in.Info)
		if err != nil {
			log.Fatalf("Failed to convert control response to int: %v", err)
		}

		fmt.Println(data)
		Calculate(data[num]) 
	}
}

// 隣接LBのフィードバック情報を取得するたびに本関数を呼び出し
// 転送するリクエスト数の計算(重み)
func Calculate(next_queue int) {
	// DC方式で計算
	diff := queue - next_queue
	weight = int(math.Round(kappa * float64(diff)))
	// weight = int(math.Round(weight))
}