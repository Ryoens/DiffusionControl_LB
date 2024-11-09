#!/bin/bash
count=0
attempt_count=1

# 各パラメータの入力
read -p "feedback: " feedback
read -p "threshold: " threshold
read -p "kappa: " kappa

echo $feedback $threshold $kappa

echo "-------- parameter OK --------"

read -p "仮想ユーザ数: " vus
read -p "number of attempts: " attempt
echo $attempt

# timestampディレクトリを作成
timestamp_data=$(date +"%Y%m%d_%H%M%S")
data_dir="../../data/${timestamp_data}"
mkdir -p "$data_dir"

# クラスタ数をコンテナ数から取得
container=$(docker ps --filter "name=_LB" --format "{{.Names}}" | head -n 1)
KEY=${container:7:1}
echo "number of clusters: " $KEY

while [ $attempt_count -le $attempt ]
do
    echo "-------- $attempt_count --------"
    count=0

    # go run $file -t $feedback -q $threshold -k $kappa
    # プログラムの実行 (gRPCが起動しない場合の挙動も必要) -> 仮想ブリッジの問題
    while [ $count -le $KEY ]
    do
        docker exec -d Cluster${count}_LB go run mirror.go -t $feedback -q $threshold -k $kappa /bin/bash
        docker exec Cluster${count}_LB ps aux # goのプロセスが走っていなかったらやり直しにしたい
        count=`expr $count + 1`
    done

    # 実験データの取得
    sleep 1
    echo $vus

    ## k6による負荷テスト
    timestamp=$(date +"%Y%m%d_%H%M%S")
    # コアごとのCPU使用率取得 (未使用)
    # mpstat -P 0-9 1 60 | awk -v OFS=',' \
    # 'BEGIN {print "Timestamp","CPU","%user","%nice","%system","%iowait","%irq","%soft","%steal","%idle"} 
    # NR>4 {print strftime("%H:%M:%S"), $3, $4, $5, $6, $7, $8, $9, $10, $NF}' > "${data_dir}/cpu_usage_${timestamp}.csv" &
    k6 run ../test.js --vus $vus --summary-export="${data_dir}/summary"_"$timestamp.json"
    # --------------------------

    ## curlで大量にリクエストを送信
    # test.shを実行したい

    for num in $(seq 2 $((count + 1)))
    do 
        i=$((num - 2)) 
        echo "Cluster$i" 
        timestamp=$(date +"%Y%m%d_%H%M%S")
        # 現在のディレクトリとは離れたところにデータを残す
        curl -X GET 114.51.4.$num:8002 -o "${data_dir}/Cluster$i"_"$timestamp.csv"
    done

    attempt_count=`expr $attempt_count + 1`
    sleep 10
done

# パラメータなどをファイルに書き出し
timestamp=$(date +"%Y%m%d_%H%M%S")
{
echo "Experiment in these parameters is finished"
echo "feedback: $feedback [ms]"
echo "threshold: $threshold"
echo "kappa: $kappa"
echo "virtual users: $vus [users]"
} > "${data_dir}/parameters"_"$timestamp".txt
