package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "custome_weightedRR/api"
)

type Server struct {
	IP     string
	Weight int
	Sessions int
	// 追加で各webサーバが持つセッション数を入れるかも
}

type Cluster struct {
	Cluster_LB string `json:"cluster_lb"`
	Web0       string `json:"web0"`
	Web1       string `json:"web1"`
	Web2       string `json:"web2"`
}

type LoadBalancer struct {
	Address   string
	IsHealthy bool
	data      int
	weight    int
	transport int
}

type Response struct {
	TotalQueue int `json:"total_queue"`
	CurrentQueue []int `json:"current_queue"`
	Data []int `json:"data"`
	Weight []int `json:"weight"`
}

type server struct {
	pb.UnimplementedLoadBalancerServer
}

// グローバル変数の定義
var (
	proxyIPs = []Server{
		{"", 0, 0},
		{"", 0, 0},
		{"", 0, 0},
	}

	clusterLBs []LoadBalancer // 隣接リスト

	randomIndex  Server
	my_clusterLB string
	queue        int // 処理待ちTCPセッション数
	currentIndex int
	wg           sync.WaitGroup
	mutex        sync.RWMutex

	// 評価用パラメータ
	total_queue int
	current_queue []int
	data []int
	weight []int

	final bool
)

const (
	// 固定値の定義
	tcp_port   string  = ":8001"
	sub_port   string  = ":8002"
	dst_port   string  = ":80"    // webサーバ用
	grpc_dest  string  = ":50051" // gRPCで使用
	sleep_time int     = 1
	threshold  int     = 700
	kappa      float64 = 0.07
)

func init() {
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

	data = make([]int, len(clusterLBs))
	weight = make([]int, len(clusterLBs))

	// 各クラスタのLBのIPアドレスと照合
	for _, cluster := range clusters {
		if clusterLB != cluster.Cluster_LB {
			// 各クラスタLBのIPアドレスをリストに追加
			clusterLBs = append(clusterLBs, LoadBalancer{
				Address:   cluster.Cluster_LB,
				IsHealthy: false,
				data:      0,
				weight:    0,
				transport: 0, 
			})
		} else {
			// proxyIPsにサーバ情報を設定
			proxyIPs[0].IP = cluster.Web0
			proxyIPs[1].IP = cluster.Web1
			proxyIPs[2].IP = cluster.Web2
			my_clusterLB = cluster.Cluster_LB
		}
	}

	// デバック用
	// Cluster2の場合: 10.0.3.10 10.0.3.11 10.0.3.12 114.51.4.4 [114.51.4.2 114.51.4.3]
	fmt.Println(proxyIPs[0].IP, proxyIPs[1].IP, proxyIPs[2].IP, my_clusterLB, clusterLBs)
}

func main(){
	wg.Add(1)
	go gRPC_Server()

	// waitgroupを利用し、複数のサーバに接続するたびにプロセスを生成
	for i, address := range clusterLBs {
		wg.Add(1)
		go gRPC_Client(address.Address, i)
	}
	
	wg.Add(1)
	go func() {
		defer wg.Done()
		s := http.Server{
			Addr:    tcp_port,
			Handler: http.HandlerFunc(lbHandler),
		}

		fmt.Printf("HTTP server is listening on %s...\n", tcp_port)
		if err := s.ListenAndServe(); err != nil {
			log.Fatal(err.Error())
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		s := http.Server{
			Addr:    sub_port,
			Handler: http.HandlerFunc(dataReceiver),
		}

		fmt.Printf("HTTP server is listening on %s...\n", sub_port)
		if err := s.ListenAndServe(); err != nil {
			log.Fatal(err.Error())
		}
	}()

	wg.Wait()
}

// リクエストをweighted RRで処理
func lbHandler(w http.ResponseWriter, r *http.Request) {
	mutex.Lock()
	total_queue++
	queue++ // 処理待ちセッション数をインクリメント
	mutex.Unlock()

	proxyURL := &url.URL{
		Scheme: "http",
		Host:   "",
	}

	if queue > threshold {
		// Calculate関数で計算した値を該当IPアドレスの重みとして指定
		randomIndex = WeightedRoundRobin_AdjacentLB()
		proxyURL.Host = randomIndex.IP + tcp_port
	} else {
		randomIndex = RoundRobin_Backend()
		proxyURL.Host = randomIndex.IP + dst_port
	}

	fmt.Println(queue)

	// デバック用(選択されたIPアドレスの確認)
	fmt.Println("Selected IP:", randomIndex, proxyURL)

	// レスポンスを書き換える
	modifier := func(res *http.Response) error {
		mutex.Lock()
		queue-- // 処理完了後にデクリメント
		mutex.Unlock()
		return nil
	}

	// make reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(proxyURL)
	proxy.ModifyResponse = modifier // ここに入れるとすぐレスポンス返却されてしまう
	proxy.ServeHTTP(w, r)
	// リバースプロキシ後に入れるとカウントがデクリメントされない
}

func dataReceiver(w http.ResponseWriter, r *http.Request) {
	if final == true {
		os.Exit(1)
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	// 負荷テスト終了後に各パラメータのデータを取得
	fmt.Printf("total_request: %d\n", total_queue)
	fmt.Printf("queue_transition: %d\n", current_queue)
	fmt.Printf("total_data: %d\n", data)
	fmt.Printf("total_weight: %d\n", weight)

	// 本来ならクラスタごとのデータを取得したい
	for i := 0; i < len(clusterLBs); i++ {
		fmt.Printf("amount of transport(%s): %d\n", clusterLBs[i].Address, clusterLBs[i].transport)
	}

	response := Response{
		TotalQueue: total_queue,
		CurrentQueue: current_queue,
		Data: data,
		Weight: weight,
	}

	fmt.Println(response)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode JSON", http.StatusInternalServerError)
		log.Println(err)
		return
	}

	final = true
	// os.Exit(1)
} 

// クラスタ間の重みづけラウンドロビン(隣接LBへの振り分け)
func WeightedRoundRobin_AdjacentLB() Server {
	// 重みは動的に変化した値を取得
	mutex.RLock()
	defer mutex.RUnlock()

	totalWeight := 0
	for _, server := range clusterLBs {
		if !server.IsHealthy {
			server.weight = 0
		}

		totalWeight += server.weight
	}

	// すべての重みが0の場合(どこの隣接LBも空いていないとき)
	if totalWeight == 0 {
		return Server{}
	}

	// 0からtotalWeight-1までの乱数を生成
	rand.Seed(time.Now().UnixNano())
	randomWeight := rand.Intn(totalWeight)

	fmt.Printf("totalWeight: %d, randomWeight: %d\n", totalWeight, randomWeight)
	// 重みでサーバーを選択
	for i, server := range clusterLBs {
		if randomWeight < server.weight {
			clusterLBs[i].transport++
			return Server{
				IP:     server.Address,
				Weight: server.weight,
			}
		}
		randomWeight -= server.weight
	}

	// ここには到達しないはずだが、デフォルトで最初のサーバーを返す
	return Server{}
}

// クラスタ内でのラウンドロビン(バックエンドサーバへの振り分け)
func RoundRobin_Backend() Server {
	list := proxyIPs[currentIndex]
	currentIndex = (currentIndex + 1) % len(proxyIPs)

	return list
}

// gRPCサーバ
func gRPC_Server() {
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
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			log.Printf("Error receiving control message: %v", err)
			return err
		}
		log.Printf("Received control command: %s, TCP Waiting Sessions: %d", in.Command, queue)

		// 現在の制御情報をクライアントに送信
		if err := stream.Send(&pb.ControlResponse{Status: "ok", Payload: int64(queue)}); err != nil {
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

// gRPCクライアント(変更前)
func gRPC_Client(address string, i int) {
	defer wg.Done()

	adjacent_lb := address + grpc_dest

	// サーバとのコネクションを確立
	conn, err := grpc.Dial(adjacent_lb, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("No connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewLoadBalancerClient(conn)

	if healthCheck(client, adjacent_lb) {
		clusterLBs[i].IsHealthy = true
		handleControlStream(client, adjacent_lb, i)
	} else {
		clusterLBs[i].IsHealthy = false
		log.Printf("Load Balancer at %s is down", adjacent_lb)
		return
	}
	// 複数LBに接続する場合、切り替えに遅延を設定...?
	// time.Sleep(time.Duration(sleep_time) * time.Second)

}

func handleControlStream(client pb.LoadBalancerClient, address string, num int) {
	//defer wg.Done()

	// ここからヘルスチェックでtrueだった場合の処理
	// 双方向ストリーミングの制御情報送受信 (streamを作成)
	stream, err := client.ControlStream(context.Background())
	if err != nil {
		log.Fatalf("Error creating stream: %v", err)
		// log.Printf("Error creating stream: %v", err)
		return
	}

	// 定期的にヘルスチェックと制御情報を送受信
	ticker := time.NewTicker(time.Duration(sleep_time) * time.Second)
	for range ticker.C {
		// 制御情報の送信
		if err := stream.Send(&pb.ControlMessage{Command: "update_policy", Payload: int64(queue)}); err != nil {
			log.Printf("Error sending control message: %v", err)
			clusterLBs[num].IsHealthy = false

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
			clusterLBs[num].IsHealthy = false

			if status.Code(err) == codes.Canceled || status.Code(err) == codes.Unavailable {
				log.Printf("Receive Connection to %s was lost, reconnecting...", address)
				return
			}
			return
		}
		log.Printf("Received control response: %d", in.Payload)

		mutex.Lock()
		clusterLBs[num].data = int(in.Payload)

		fmt.Println(clusterLBs[num].data, clusterLBs)
		Calculate(clusterLBs[num].data, num)
		mutex.Unlock()
	}
}

// 隣接LBのフィードバック情報を取得するたびに本関数を呼び出し
// 転送するリクエスト数の計算(重み)
func Calculate(next_queue int, num int) {
	// DC方式で計算

	if queue > next_queue {
		diff := queue - next_queue
		clusterLBs[num].weight = int(math.Round(kappa * float64(diff)))
	} else {
		clusterLBs[num].weight = 0
	}

	current_queue = append(current_queue, queue)
	data = append(data, clusterLBs[num].data)
	weight = append(weight, clusterLBs[num].weight)
}