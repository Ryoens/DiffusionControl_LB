// ラウンドロビン方式(静的負荷分散)に基づき隣接LB間で負荷分散
package main

import (
	// "context"
	"encoding/json"
	"fmt"
	"flag"
	// "io"
	"io/ioutil"
	"log"
	// "math"
	// "math/rand"
	// "net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"strconv"
	"sync"
	"time"
)

type Server struct {
	IP     string
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
	transport int
}

type Response struct {
	TotalQueue []int // 総リクエスト数
	CurrentQueue []int // セッション数
	CurrentResponse []int // レスポンス数
	CurrentTransport []int // 転送数
	Transport []int
}

type Split_data struct {
	Data []int
	Weight []int
	Transport []int
}

// グローバル変数の定義
var (
	proxyIPs = []Server{
		{"", 0},
		{"", 0},
		{"", 0},
	}

	clusterLBs []LoadBalancer // 隣接リスト

	randomIndex  Server
	my_clusterLB string
	BackendIndex int // RR方式におけるインデックス
	AdjacentIndex int 
	wg           sync.WaitGroup
	mutex        sync.RWMutex

	// 評価用パラメータ
	queue        int // 処理待ちTCPセッション数
	total_queue int // LBに入ってきた全てのリクエスト
	res_count    int // レスポンス返却した数をカウント	
	web_count int // 内部のwebサーバにてレスポンス返却した数
	current_transport int // リバースプロキシで隣接LBに転送した数

	total_data []int // 
	current_queue []int
	current_response []int
	total_transport []int
	transport []int

	final bool
	transport_dest bool
	threshold int
)

const (
	// 固定値の定義
	tcp_port   string  = ":8001"
	sub_port   string  = ":8002"
	dst_port   string  = ":80"    // webサーバ用
	sleep_time time.Duration = 1
	getdata_time time.Duration = 100
)

func init() {
	// 引数の取得
	q := flag.Int("q", 0, "threshold")

	flag.Parse()
    threshold = *q

	fmt.Printf("threshold -q : %d\n", threshold)

	// JSONファイルを開く
	file, err := os.Open("../json/config.json")
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

	transport = make([]int, len(clusterLBs))

	// 各クラスタのLBのIPアドレスと照合
	for _, cluster := range clusters {
		if clusterLB != cluster.Cluster_LB {
			// 各クラスタLBのIPアドレスをリストに追加
			clusterLBs = append(clusterLBs, LoadBalancer{
				Address:   cluster.Cluster_LB,
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

	wg.Add(1)
	go func() {
		defer wg.Done()
		getData()
	}()

	wg.Wait()
}

func getData() {
	for {
		if final {
			os.Exit(1)
		}

		total_data = append(total_data, total_queue)
		current_queue = append(current_queue, queue)
		current_response = append(current_response, res_count)
		// web_response = append(web_response, web_count)
		total_transport = append(total_transport, current_transport)

		for _, server := range clusterLBs {
			transport = append(transport, server.transport)
		}

		time.Sleep(getdata_time * time.Millisecond) // ms
		// time.Sleep(time.Duration(sleep_time) * time.Second) // s
	}
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

	// リバースプロキシを作成
	proxy := httputil.NewSingleHostReverseProxy(proxyURL)

	if queue > threshold {
		// Calculate関数で計算した値を該当IPアドレスの重みとして指定
		randomIndex = RoundRobin_AdjacentLB()
		proxyURL.Host = randomIndex.IP + tcp_port

		proxy.ModifyResponse = func(res *http.Response) error {
			mutex.Lock()
			queue-- // 処理完了後にデクリメント
			current_transport++
			mutex.Unlock()
			return nil
		}
		
	} else {
		randomIndex = RoundRobin_Backend()
		proxyURL.Host = randomIndex.IP + dst_port

		// レスポンスを書き換える -> 内部のwebサーバへ送る場合
		proxy.ModifyResponse = func(res *http.Response) error {
			mutex.Lock()
			queue-- // 処理完了後にデクリメント
			res_count++ 
			mutex.Unlock()
			return nil
		}
	}
	// レスポンス返却までに遅延を設定?
	// time.Sleep(time.Duration(feedback) * time.Millisecond)

	// リバースプロキシで各転送先へリクエスト移譲
	proxy.ServeHTTP(w, r) // webサーバからレスポンス返却でres_countがインクリメント
	//fmt.Println(queue, res_count, current_transport)
}

func dataReceiver(w http.ResponseWriter, r *http.Request) {
	// 現在のセッション数を取得
	// last_sessions := queue
	// last_response := res_count
	
	// 負荷テスト終了後に各パラメータのデータを取得
	fmt.Printf("total_request: %d\n", total_queue)
	fmt.Printf("queue_transition: %d\n", current_queue)
	
	// 本来ならクラスタごとのデータを取得したい
	for i := 0; i < len(clusterLBs); i++ {
		fmt.Printf("amount of transport(%s): %d\n", clusterLBs[i].Address, clusterLBs[i].transport)
	}

	clusters := make([]Split_data, len(clusterLBs))

	for i := 0; i < len(transport); i++ {
		clusterIndex := i % len(clusterLBs)
		clusters[clusterIndex].Transport = append(clusters[clusterIndex].Transport, transport[i])
	}

	response := Response{
		TotalQueue: total_data,
		CurrentQueue: current_queue,
		CurrentResponse: current_response,
		CurrentTransport: total_transport,
		Transport: transport,
	}

	// --------
	filename := "../log/output.csv"
	file, err := os.Create(filename)
	if err != nil {
		fmt.Println("failure creating csv file:", err)
		return
	}
	defer file.Close()

	var csvData strings.Builder
	header := []string{"TotalQueue"}
	header = append(header, "CurrentQueue")
	header = append(header, "CurrentResponse")
	header = append(header, "CurrentTransport")
	for i := 0; i < len(clusterLBs); i++ {
		header = append(header, fmt.Sprintf("%d_Transport", i))
	}
	
	// パラメータが増えた場合はcsv出力としてここで追加する
	csvData.WriteString(strings.Join(header, ",") + "\n")

	rowCount := len(response.CurrentQueue)

	for i := 0; i < rowCount; i++ {
		record := []string{fmt.Sprint(response.TotalQueue[i])}
		record = append(record, strconv.Itoa(response.CurrentQueue[i]))
		record = append(record, strconv.Itoa(response.CurrentResponse[i]))
		record = append(record, strconv.Itoa(response.CurrentTransport[i]))
		
		for j := 0; j < len(clusterLBs); j++ {
			if i < len(clusters[j].Transport) {
				record = append(record, strconv.Itoa(clusters[j].Transport[i]))
			} else {
				record = append(record, "0") // 値がない場合は0を挿入
			}
		}

		csvData.WriteString(strings.Join(record, ",") + "\n") 
	}

	_, err = file.WriteString(csvData.String())
	if err != nil {
		fmt.Println("Error writing to CSV file:", err)
		return
	}
	// --------
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)

	http.ServeFile(w, r, filename)

	final = true
	// os.Exit(1)
} 

// ウェイトのスライスをカンマ区切りの文字列に変換するヘルパー関数
func joinWeight(weight []int) string {
	var result strings.Builder
	for i, w := range weight {
		if i > 0 {
			result.WriteString(",")
		}
		result.WriteString(strconv.Itoa(w))
	}
	return result.String()
}

// クラスタ間でのラウンドロビン(隣接LBへの振り分け)
func RoundRobin_AdjacentLB() Server {
	extenal := clusterLBs[AdjacentIndex]
	AdjacentIndex = (AdjacentIndex + 1) % len(clusterLBs)

	// external: LoadBalancer型 -> Server型に変換
	return Server{
		IP:       extenal.Address,
		Sessions: extenal.transport,
	}
}

// クラスタ内でのラウンドロビン(バックエンドサーバへの振り分け)
func RoundRobin_Backend() Server {
	// proxyIPs[currentIndex].Sessions++
	internal := proxyIPs[BackendIndex]
	BackendIndex = (BackendIndex + 1) % len(proxyIPs)

	return internal
}