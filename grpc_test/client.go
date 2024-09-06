// クライアント側
package main

import (
	"context"
	"google.golang.org/grpc"
	"log"
	"time"

	pb "grpc_test/pkg/grpc"
)

func main() {
	// サーバとのコネクションを確立
	conn, err := grpc.Dial("localhost:50051", grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("Did not connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewLoadBalancerClient(conn)

	// 双方向ストリーミングの制御情報送受信
	stream, err := client.ControlStream(context.Background())
	if err != nil {
		log.Fatalf("Error creating stream: %v", err)
	}

	// 定期的にヘルスチェックと制御情報を送受信
	ticker := time.NewTicker(10 * time.Second)
	for range ticker.C {
		// ヘルスチェック
		req := &pb.BackendRequest{ServerName: "backend-1"}
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
	}
}