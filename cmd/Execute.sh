#!/bin/bash
count=0
attempt_count=1
time=60

# 各パラメータの入力
read -p "feedback [ms]: " feedback 
read -p "threshold [0:100]: " threshold
read -p "kappa [float]: " kappa

# 小数点をアンダーバーに置換
safe_kappa=$(echo "$kappa" | sed 's/\./_/g')
echo $feedback $threshold $kappa $safe_kappa

echo "-------- parameter OK --------"

read -p "concurrent: " vus
read -p "number of attempts [5 times]: " attempt
echo $vus $attempt

echo "-------- request OK --------"

# クラスタ数をコンテナ数から取得
container=$(docker ps --filter "name=_LB" --format "{{.Names}}" | head -n 1)
KEY=${container:7:1}
echo "number of clusters: " $KEY

read -p "Target Cluster for Flash Crowds [0:$KEY]: " cluster
if ! [[ "$cluster" =~ ^[0-9]+$ ]] || [ "$cluster" -ge "$KEY" ]; then
  echo "Invalid input. Please enter a number between 0 and $((KEY - 1))."
  exit 1
fi
    
ip_last=$((2 + cluster))
url="http://114.51.4.${ip_last}:8001"
echo "Target URL: $url"

echo "-------- URL OK --------"

# 対象ファイルの指定
read -p "file to apply [t: DC(threshold), d: DC(diff), r: RR, l: LC]: " file
case "$file" in
  t)
    apply_file="lb_thre.go"
    ;;
  d)
    apply_file="lb_diff.go"
    ;;
  r)
    apply_file="lb_rr.go"
    ;;
  l)
    apply_file="lb_lc.go"
    ;;
  *)
    echo "Invalid input. Please enter one of: t, d, r, l"
    exit 1
    ;;
esac

compiled_file="${apply_file%.go}"

echo $apply_file $compiled_file

# timestampディレクトリを作成
timestamp_data=$(date +"%Y%m%d_%H%M%S")
read -p "parameter to be changed:" type # request, threshold, kappa
dirname="t${feedback}_t${threshold}_k${safe_kappa}_vus${vus}"
# dirname="t${feedback}_k${safe_kappa}_vus${vus}"
data_dir="../../data/${type}/${dirname}"
mkdir -p "$data_dir"

echo $timestamp $dirname $data_dir
count=0

# nameserverの設定, build
for count in $(seq 0 "$KEY");
do 
    docker exec Cluster${count}_LB sh -c 'echo "nameserver 8.8.8.8" > /etc/resolv.conf'
    docker exec Cluster${count}_LB cat /etc/resolv.conf
    docker exec Cluster${count}_LB sh -c "go build -o compiled/$compiled_file lb/$apply_file"
done

echo "-------- Build OK --------"

for count in $(seq 0 "$KEY");
do
    docker exec Cluster${count}_LB ls -l compiled/$compiled_file
    docker exec Cluster${count}_LB ps aux
done

sleep 5
exit 1

while [ $attempt_count -le $attempt ]
do
    echo "-------- $attempt_count --------"
    count=0

    # go run $file -t $feedback -q $threshold -k $kappa
    # プログラムの実行 (gRPCが起動しない場合の挙動も必要) -> 仮想ブリッジの問題
    # for文に変更 
    # while [ $count -le $KEY ]
    # for ((count=0; count<=KEY; count++));
    for count in $(seq 0 "$KEY");
    do
        docker exec -d Cluster${count}_LB ./$compiled_file -t $feedback -q $threshold -k $kappa /bin/bash
        # docker exec -d Cluster${count}_LB go run $file -t $feedback -k $kappa /bin/bash
        docker exec Cluster${count}_LB ps aux # goのプロセスが走っていなかったらやり直しにしたい
        # count=`expr $count + 1`
    done

    # 実験データの取得
    # sleep 1
    echo $vus
    # --------------------------
    # 負荷テスト(apache bench, apache jmeter, curl, wrk, etc...)

    # apache benchによる負荷テスト
    timestamp=$(date +"%Y%m%d_%H%M%S")
    # 時間指定: -t 60
    ab -c $vus -n $req -k http://114.51.4.2:8001/ > "${data_dir}/result_${timestamp}.html"
    wait
    echo "All tests completed."

    # apache jmeterによる負荷テスト
    ./jmeter.sh http://114.51.4.2:8001/ $time $vus
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
    rm ./log/output.csv
    docker exec -it redis-server redis-cli flushall # rediskeyの初期化
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
