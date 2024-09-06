// サーバ側
package main

import (
	"context"
	"fmt"
	"google.golang.org/grpc"
	"log"
	"net"
	"time"
	"math/rand"

	pb "grpc_test/pkg/grpc"
)

type server struct {
	pb.UnimplementedLoadBalancerServer
}

// バックエンドサーバー(隣接LB)のヘルスチェック
func (s *server) GetBackendStatus(ctx context.Context, req *pb.BackendRequest) (*pb.BackendStatus, error) {
	fmt.Printf("Received health check request for server: %s\n", req.ServerName)
	return &pb.BackendStatus{IsHealthy: true}, nil
}

// 制御情報(フィードバック情報)の送信
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

func main() {
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterLoadBalancerServer(s, &server{})
	log.Printf("Server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}