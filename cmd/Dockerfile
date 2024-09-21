FROM ubuntu:latest

ENV TZ=Asia/Tokyo
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone

RUN apt-get update
RUN apt-get upgrade -y

RUN apt-get install -y autoconf automake build-essential libssl-dev libtool pkg-config
RUN apt-get install -y cmake curl g++ git golang-go iputils-ping libtool make net-tools unzip wget protobuf-compiler

WORKDIR /
RUN mkdir -p .local
RUN wget -q -O cmake-linux.sh https://github.com/Kitware/CMake/releases/download/v3.17.0/cmake-3.17.0-Linux-x86_64.sh
RUN sh cmake-linux.sh -- --skip-license --prefix=.local
RUN rm cmake-linux.sh

WORKDIR /home/ubuntu
