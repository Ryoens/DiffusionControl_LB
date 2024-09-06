package main

import(
	"fmt"
	"log"
	"math/rand"
	"time"
	// "net/url"
	"net/http"
	// "net/http/httputil"
)

type Server struct {
	IP	string 
	Weight	int
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
)

func main(){
	// http.HandleFunc("/", lbHandler)
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

// ヘルスチェックと同時に制御情報を受信
func GetFeedback() {

}

// 転送するリクエスト数の計算(重み)
func Calculate() {
	
}