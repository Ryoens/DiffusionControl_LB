FROM ubuntu:latest

ENV TZ=Asia/Tokyo \
    DEBIAN_FRONTEND=noninteractive

RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone \
    && apt-get update \
    && apt-get upgrade -y \
    && apt-get install -y --no-install-recommends \
        autoconf \
        automake \
        build-essential \
        ca-certificates \
        cmake \
        curl \
        g++ \
        git \
        golang-go \
        iputils-ping \
        iproute2 \
        libssl-dev \
        libtool \
        make \
        net-tools \
        pkg-config \
        protobuf-compiler \
        unzip \
        wget \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /home/ubuntu
