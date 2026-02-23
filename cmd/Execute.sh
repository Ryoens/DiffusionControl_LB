#!/bin/bash
count=0
attempt_count=1
time=60
source ~/.bashrc
mkdir -p ../log ../compiled ../data

# input each parameters
read -p "feedback [ms]: " feedback 
read -p "threshold [0:100]: " threshold
read -p "kappa [float]: " kappa

# replace decimal point with underscore
safe_kappa=$(echo "$kappa" | sed 's/\./_/g')
echo $feedback $threshold $kappa $safe_kappa

echo "-------- parameter OK --------"

read -p "concurrent: " vus
read -p "number of attempts [5 times]: " attempt
echo $vus $attempt

echo "-------- request OK --------"

# set NW model
read -p "NW model [f: fullmesh, r: random, ba: balabasi and albert]: " nw_model
echo $nw_model
echo "-------- NW model OK --------"

# set number of web servers
read -p "Number of Clusters to reduce Web Servers: " num_cluster

if [[ $num_cluster -eq 0 ]]; then
  echo "default start"
elif [[ $num_cluster -eq 1 ]]; then
  read -p "Set number [Cluster] [WebServers]: " cls web
else
  cls=()
  web=()
  for num_cls in $(seq 0 $((num_cluster - 1)) )
  do
    read -p "Set number [Cluster] [WebServers]: " temp_cls temp_web
    cls+=($temp_cls)
    web+=($temp_web)
  done
fi

echo ${cls[@]} ${web[@]}

# create adjacency list
python3 ../tools/adjacentListController.py $nw_model ${cls[@]} ${web[@]}
echo "Created adjacentList per cluster"

# set delay between clusters (manual setting)
python3 ../tools/delayController.py
echo "Set delay between clusters"

# get number of clusters from container count
container=$(docker ps --filter "name=_LB" --format "{{.Names}}" | head -n 1)
# KEY=${container:7:1}
KEY=$(echo "$container" | sed -E 's/^Cluster([0-9]+)_LB$/\1/')
echo "number of clusters: " $KEY

read -p "Target Cluster for Flash Crowds [0:$KEY]: " cluster
if ! [[ "$cluster" =~ ^[0-9]+$ ]] || [ "$cluster" -ge "$KEY" ]; then
  echo "Invalid input. Please enter a number between 0 and $((KEY - 1))."
  exit 1
fi
    
ip_last=$((2 + cluster))
url="http://172.18.4.${ip_last}:8001"
echo "Target URL: $url"

echo "-------- URL OK --------"

# set target file to apply
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

# create directory for saving measurement results
dirname="${compiled_file}_t${feedback}_t${threshold}_k${safe_kappa}_vus${vus}"
data_dir="../data/implement/${dirname}"
mkdir -p "$data_dir"

echo $dirname $data_dir
count=0

# set nameserver, build
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
        docker exec -d Cluster${count}_LB compiled/$compiled_file $cluster -t $feedback -q $threshold -k $kappa /bin/bash
        docker exec Cluster${count}_LB ps aux # goのプロセスが走っていなかったらやり直しにしたい
    done

    while true;
    do
      key_list=$(docker exec -i redis-server redis-cli --raw keys 'lb_ready:*')
      key_count=$(echo "$key_list" | grep -c '^lb_ready:')
      echo $key_count

      if [ "$key_count" -eq $((KEY + 1)) ]; then
        echo "start"
        break
      fi
      sleep 1
    done

    # load test using apache jmeter
    ./../tools/jmeter_multi.sh $url $time $vus $KEY
    wait
    echo "All tests completed."

    echo $count
    for num in $(seq 2 $((count + 2)))
    do 
        i=$((num - 2)) 
        echo "Cluster$i" 
        timestamp=$(date +"%Y%m%d_%H%M%S")
        # Save data in a directory separate from the current directory
        curl -X GET 172.18.4.$num:8002 -o "${data_dir}/Cluster$i"_"$attempt_count"_"$timestamp.csv"
    done

    # move measurement result files
    timestamp=$(date +"%Y%m%d_%H%M%S")
    rm -f ../log/output.csv temp_test.jmx
    mv ../log/jmeter.log "${data_dir}/jmeter_${attempt_count}_${timestamp}.log"
    mv ../log/result_60s.jtl "${data_dir}/jmeter_result${time}s_${attempt_count}_${timestamp}.jtl"

    # initialize redis keys
    docker exec -it redis-server redis-cli flushall 
    attempt_count=`expr $attempt_count + 1`
    sleep 5
done

# data formatting
echo $data_dir
python3 ../tools/to_average.py $data_dir $KEY
python3 ../tools/to_median.py $data_dir $KEY

# write parameters and other information to a file
timestamp=$(date +"%Y%m%d_%H%M%S")
{
echo "Experiment in these parameters is finished"
echo "feedback: $feedback [ms]"
echo "threshold: $threshold"
echo "kappa: $kappa"
echo "virtual users: $vus [users]"
echo "network model: $nw_model"
} > "${data_dir}/parameters"_"$timestamp".txt
