// サーバ側
package main

import (
	"context"
	"fmt"
	"google.golang.org/grpc"
	"log"
	"net"
	// "time"

	pb "grpc_test/pkg/grpc"
)

type server struct {
	pb.UnimplementedLoadBalancerServer
}

func (s *server) GetBackendStatus(ctx context.Context, req *pb.BackendRequest) (*pb.BackendStatus, error) {
	// バックエンドサーバーの状態をチェックするロジック
	fmt.Printf("Received health check request for server: %s\n", req.ServerName)
	return &pb.BackendStatus{IsHealthy: true}, nil
}

func (s *server) ControlStream(stream pb.LoadBalancer_ControlStreamServer) error {
	for {
		in, err := stream.Recv()
		if err != nil {
			log.Printf("Error receiving control message: %v", err)
			return err
		}
		// 制御情報を処理し、必要ならレスポンスを返す
		log.Printf("Received control command: %s", in.Command)
		if in.Command == "update_policy" {
			stream.Send(&pb.ControlResponse{Status: "ok", Info: "Policy updated"})
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