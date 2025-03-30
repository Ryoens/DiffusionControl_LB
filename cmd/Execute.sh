#!/bin/bash
count=0
attempt_count=1
time=60

# 各パラメータの入力
read -p "feedback: " feedback
read -p "threshold: " threshold
read -p "kappa: " kappa

# 小数点をアンダーバーに置換
safe_kappa=$(echo "$kappa" | sed 's/\./_/g')
echo $feedback $threshold $kappa $safe_kappa

echo "-------- parameter OK --------"

read -p "concurrent: " vus
read -p "total requests: " req
# req=$(echo "$vus * $time" | bc)
read -p "number of attempts: " attempt
echo $vus $req $attempt

echo "-------- request OK --------"

# timestampディレクトリを作成
read -p "file to apply: " file # lb_diff.go, lb_thre.go
timestamp_data=$(date +"%Y%m%d_%H%M%S")
read -p "parameter to be changed:" type # request, threshold, kappa
dirname="t${feedback}_t${threshold}_k${safe_kappa}_vus${vus}_req${req}"
# dirname="t${feedback}_k${safe_kappa}_vus${vus}"
data_dir="../../data/${type}/${dirname}"
mkdir -p "$data_dir"

# クラスタ数をコンテナ数から取得
container=$(docker ps --filter "name=_LB" --format "{{.Names}}" | head -n 1)
KEY=${container:7:1}
echo "number of clusters: " $KEY

count=0
compiled_file="${file%.go}"
# gRPCのビルド
while [ $count -le $KEY ]
do
    docker exec -d Cluster${count}_LB go build -o $compiled_file $file /bin/bash
    docker exec Cluster${count}_LB ps aux
    count=`expr $count + 1`
done
echo "build OK"
sleep 5

while [ $attempt_count -le $attempt ]
do
    echo "-------- $attempt_count --------"
    count=0

    # go run $file -t $feedback -q $threshold -k $kappa
    # プログラムの実行 (gRPCが起動しない場合の挙動も必要) -> 仮想ブリッジの問題
    while [ $count -le $KEY ]
    do
        docker exec -d Cluster${count}_LB ./$compiled_file -t $feedback -q $threshold -k $kappa /bin/bash
        # docker exec -d Cluster${count}_LB go run $file -t $feedback -k $kappa /bin/bash
        docker exec Cluster${count}_LB ps aux # goのプロセスが走っていなかったらやり直しにしたい
        count=`expr $count + 1`
    done

    # 実験データの取得
    sleep 1
    echo $vus
    # --------------------------
    # 負荷テスト(apache bench, curl, wrk, etc...)

    # apache benchによる負荷テスト
    timestamp=$(date +"%Y%m%d_%H%M%S")
    # 時間指定: -t 60
    ab -c $vus -n $req -k http://114.51.4.2:8001/ > "${data_dir}/result_${timestamp}.html"
    wait
    echo "All tests completed."

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
echo "total requests: $req [requests]"
} > "${data_dir}/parameters"_"$timestamp".txt
