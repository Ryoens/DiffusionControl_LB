#!/bin/bash

KEY=0
QTY_CLUSTER=$(($1))

docker image build ./ -t lb:latest

docker network rm gushing-ecstasy
docker network create gushing-ecstasy --driver=bridge --subnet=114.51.4.0/24

protoc --version
echo $QTY_CLUSTER
pwd

while [ $KEY -le $QTY_CLUSTER ]
do
	sudo mkdir -p ./cluster$KEY
	sudo chmod 777 ./cluster$KEY
	sudo sed -e "s/@num@/$((KEY))/g" -e "s/@num+1@/$((KEY+1))/g" ./docker-compose.yml > ./cluster$KEY/docker-compose.yml

	cd ./cluster$KEY
	docker compose up -d
	cd ../
	rm -rf ./cluster$KEY

	# sudo docker exec Cluster$((KEY))_LB rm -rf cmd/ // コンテナ内でcmd/を消したい

	KEY=`expr $KEY + 1`
done

