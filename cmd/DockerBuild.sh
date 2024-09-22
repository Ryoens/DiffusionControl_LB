#!/bin/bash

function json_output () {
	container_name="Cluster$((KEY))_LB"

  	IP_Addresses=$(docker exec $container_name hostname -i)
  	echo $IP_Addresses

  	IP_Array=($IP_Addresses)

  	cat <<EOF >> $JSON_FILE
  	"cluster$KEY": {
    	"cluster_lb": "${IP_Array[1]}",
    	"web0": "10.0.$((KEY+1)).10",
    	"web1": "10.0.$((KEY+1)).11",
    	"web2": "10.0.$((KEY+1)).12"
  	}$(if [ $KEY -lt $QTY_CLUSTER ]; then echo ","; fi)
EOF

  	echo "IP addresses saved to cluster_ips.json"
}

# ----------------------

KEY=0
JSON_FILE="../json/config.json"
QTY_CLUSTER=$(($1))

docker image build ./ -t lb:latest

docker network rm gushing-ecstasy
docker network create gushing-ecstasy --driver=bridge --subnet=114.51.4.0/24

protoc --version
echo $QTY_CLUSTER
pwd

echo "{" > $JSON_FILE

while [ $KEY -le $QTY_CLUSTER ]
do
	sudo mkdir -p ./cluster$KEY
	sudo chmod 777 ./cluster$KEY
	sudo sed -e "s/@num@/$((KEY))/g" -e "s/@num+1@/$((KEY+1))/g" ./docker-compose.yml > ./cluster$KEY/docker-compose.yml

	cd ./cluster$KEY
	docker compose up -d
	cd ../
	rm -rf ./cluster$KEY

	# クラスタごとのIPアドレスをJSONで保持
	json_output

	KEY=`expr $KEY + 1`
done

echo "}" >> $JSON_FILE


