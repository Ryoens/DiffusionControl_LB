package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
)

type Server struct {
	IP	string
	Weight	int
}

type Cluster struct {
	Cluster_LB string `json:"cluster_lb"`
	Web0      string `json:"web0"`
	Web1      string `json:"web1"`
	Web2      string `json:"web2"`
}

// グローバル変数の定義
var (
	proxyIPs = []Server{
		{"", 0}, // 初期状態は空のIP
		{"", 0},
		{"", 0},
	}
	clusterLBs []string
)
// var clusters map[string]Cluster

func init(){
	// JSONファイルを開く
	file, err := os.Open("config.json")
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

	fmt.Println("Cluster data loaded successfully")

	// hostname -i を実行
	cmd := exec.Command("hostname", "-i")
	output, err := cmd.Output()
	if err != nil {
		fmt.Printf("Error executing command: %v\n", err)
		return
	}

	// 出力結果をスペースで分割し、配列に代入
	ip_addresses := strings.Fields(string(output))

	fmt.Println("Extracted IP addresses:")
	for _, ip_address := range ip_addresses {
		fmt.Println(ip_address)
	}

	// 自身のCluster_LBのIPアドレスを抽出
	clusterLB := ip_addresses[1] // 最初のIPアドレスを取得
	fmt.Printf("Cluster Load Balancer IP: %s\n", clusterLB)

	// 各クラスタのLBのIPアドレスと照合
	for clusterName, cluster := range clusters {
		// 各クラスタLBのIPアドレスをリストに追加
		clusterLBs = append(clusterLBs, cluster.Cluster_LB)

		if clusterLB == cluster.Cluster_LB {
			// proxyIPsにサーバ情報を設定
			proxyIPs[0].IP = cluster.Web0
			proxyIPs[1].IP = cluster.Web1
			proxyIPs[2].IP = cluster.Web2

			fmt.Printf("\nMatching cluster found: %s\n", clusterName)
			fmt.Printf("Cluster LB IP: %s\n", cluster.Cluster_LB)
			fmt.Printf("  Web0: %s\n", proxyIPs[0].IP)
			fmt.Printf("  Web1: %s\n", proxyIPs[1].IP)
			fmt.Printf("  Web2: %s\n", proxyIPs[2].IP)
		}
	}
}

func main() {
	fmt.Println("\nCluster Load Balancers:")
	for _, lb := range clusterLBs {
		fmt.Printf("LB IP: %s\n", lb)
	}
}