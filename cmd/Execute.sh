#!/bin/bash
count=0
attempt_count=1
time=60
source ~/.bashrc

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

# 計測結果の保存用ディレクトリ作成
dirname="${compiled_file}_t${feedback}_t${threshold}_k${safe_kappa}_vus${vus}"
data_dir="../../data/implement/${dirname}"
mkdir -p "$data_dir"

echo $dirname $data_dir
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

while [ $attempt_count -le $attempt ]
do
    echo "-------- $attempt_count --------"
    count=0

    for count in $(seq 0 "$KEY");
    do
        docker exec -d Cluster${count}_LB compiled/$compiled_file -t $feedback -q $threshold -k $kappa /bin/bash
        docker exec Cluster${count}_LB ps aux # goのプロセスが走っていなかったらやり直しにしたい
    done

    sleep 1
    # 実験データの取得
    # --------------------------
    ## 負荷テスト(apache bench, apache jmeter, curl, wrk, etc...)

    # apache jmeterによる負荷テスト
    ./../tools/jmeter_multi.sh $url $time $vus
    wait
    echo "All tests completed."

    echo $count
    for num in $(seq 2 $((count + 2)))
    do 
        i=$((num - 2)) # cluster 5台: i=0:4
        echo "Cluster$i" 
        timestamp=$(date +"%Y%m%d_%H%M%S")
        # 現在のディレクトリとは離れたところにデータを残す
        curl -X GET 114.51.4.$num:8002 -o "${data_dir}/Cluster$i"_"$attempt_count"_"$timestamp.csv"
    done

    # 計測結果ファイルの移動
    timestamp=$(date +"%Y%m%d_%H%M%S")
    rm -f ../log/output.csv temp_test.jmx
    mv ../log/jmeter.log "${data_dir}/jmeter_${attempt_count}_${timestamp}.log"
    mv ../log/result_60s.jtl "${data_dir}/jmeter_result${time}s_${attempt_count}_${timestamp}.jtl"

    docker exec -it redis-server redis-cli flushall # rediskeyの初期化
    attempt_count=`expr $attempt_count + 1`
    sleep 5
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
