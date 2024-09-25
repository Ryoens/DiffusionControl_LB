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
}

func main(){
	var wg sync.WaitGroup

	wg.Add(1)
	go gRPC_Server(&wg)

	for i, address := range clusterLBs {
		wg.Add(1)
		go gRPC_Client(address, i, &wg)
	}
	
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
	// ランダムシードを設定
	queue++ // 処理待ちセッション数をインクリメント

	if queue > threshold {
		// Calculate関数で計算した値を該当IPアドレスの重みとして指定
	} else {
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

// gRPCクライアント
func gRPC_Client(address string, i int, wg *sync.WaitGroup) {
	defer wg.Done()

	adjacent_lb := address + grpc_dest

	// サーバとのコネクションを確立
	conn, err := grpc.Dial(adjacent_lb, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("No connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewLoadBalancerClient(conn)

	// 双方向ストリーミングの制御情報送受信
	stream, err := client.ControlStream(context.Background())
	if err != nil {
		log.Fatalf("Error creating stream: %v", err)
	}

	// 定期的にヘルスチェックと制御情報を送受信
	ticker := time.NewTicker(time.Duration(sleep_time) * time.Second)
	for range ticker.C {
		// ヘルスチェック
		req := &pb.BackendRequest{ServerName: adjacent_lb} // 本当は宛先サーバにしたい
		res, err := client.GetBackendStatus(context.Background(), req)
		if err != nil {
			log.Printf("Could not get backend status: %v", err)
		} else {
			log.Printf("Backend is healthy: %v", res.IsHealthy)
		}

		// 制御情報の送信
		if err := stream.Send(&pb.ControlMessage{Command: "update_policy", Payload: "new_policy_data"}); err != nil {
			log.Fatalf("Error sending control message: %v", err)
		}

		// 制御情報の応答受信
		in, err := stream.Recv()
		if err != nil {
			log.Fatalf("Error receiving control response: %v", err)
		}
		log.Printf("Received control response: %s", in.Info)
		
		data[i], err = strconv.Atoi(in.Info)
		if err != nil {
			log.Fatalf("Failed to convert control response to int: %v", err)
		}

		fmt.Println(data)
		Calculate(data[i]) 
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