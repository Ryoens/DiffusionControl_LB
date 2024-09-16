package main

import(
	"fmt"
	"log"
	"time"
	"net"
	"context"
	"math/rand"
	// "net/url"
	"net/http"
	// "net/http/httputil"
	"google.golang.org/grpc"

	pb "custome_weightedRR/api"
)

type Server struct {
	IP	string 
	Weight	int
}

type server struct {
	pb.UnimplementedLoadBalancerServer
}

var (
	// グローバル変数の定義
	proxyIPs = []Server{
		{"172.30.0.11", 0},
		{"172.30.0.12", 0}, 
		{"172.30.0.13", 0},
	}
)

const (
	// 固定値の定義
	tcp_port string = ":8001"
	sleep_time int = 1
)

func main(){
	// http.HandleFunc("/", lbHandler)
	go gRPC_Server()

	s := http.Server{
		Addr:	tcp_port,
		Handler: http.HandlerFunc(lbHandler),
	}

	fmt.Printf("HTTP server is listening on %s...\n", tcp_port)
	if err := s.ListenAndServe(); err != nil {
		log.Fatal(err.Error())
	}
}

// リクエストをweighted RRで処理
func lbHandler(w http.ResponseWriter, r *http.Request) {
	// ランダムシードを設定
	rand.Seed(time.Now().UnixNano())

	for i := range proxyIPs{
		proxyIPs[i].Weight = rand.Intn(10)+1
	}

	for _, server := range proxyIPs {
		fmt.Printf("IP: %s, Weight: %d\n", server.IP, server.Weight)
	}

	randomIndex := WeightedRoundRobin()
	fmt.Println("Selected IP:", randomIndex)
	fmt.Printf("---\n")
	// proxyURL := &url.URL {
	// 	Scheme: "http",
	// 	Host: proxyIPs[randomIndex] + tcp_port,
	// }

	// make reverse proxy
	// proxy := httputil.NewSingleHostReverseProxy(proxyURL)
	// proxy.ServeHTTP(w, r)
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
func gRPC_Server() {
	lis, err := net.Listen("tcp", ":50051")
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
		if err := stream.Send(&pb.ControlResponse{Status: "ok", Info: fmt.Sprintf("Random value: %d", randomControlValue)}); err != nil {
			log.Printf("Error sending response: %v", err)
			return err
		}
	}
}

// gRPCクライアント
func client() {

}

// ヘルスチェックと同時に制御情報を受信
func GetFeedback() {

}

// 転送するリクエスト数の計算(重み)
func Calculate() {
	
}