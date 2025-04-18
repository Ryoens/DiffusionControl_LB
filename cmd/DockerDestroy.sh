#!/bin/bash

KEY=0
QTY_CLUSTER=$(($1))

docker stop redis-server
docker rm redis-server

echo $QTY_CLUSTER

while [ $KEY -le $QTY_CLUSTER ]
do
	sudo mkdir -p ./cluster$KEY
	sudo chmod 777 ./cluster$KEY
	sudo sed -e "s/@num@/$((KEY))/g" -e "s/@num+1@/$((KEY+1))/g" ./docker-compose.yml > ./cluster$KEY/docker-compose.yml

	cd ./cluster$KEY
	docker compose down
	cd ../
	rm -rf ./cluster$KEY

	KEY=`expr $KEY + 1`
done