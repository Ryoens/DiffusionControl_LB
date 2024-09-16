FROM ubuntu:latest

ENV TZ=Asia/Tokyo
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone



RUN apt-get update
RUN apt-get upgrade -y

RUN apt-get install -y autoconf automake build-essential libssl-dev libtool pkg-config
RUN apt-get install -y cmake curl g++ git golang-go iputils-ping libtool make net-tools unzip wget



RUN git clone -b 3.6.x https://github.com/protocolbuffers/protobuf.git

WORKDIR /protobuf
RUN git submodule update --init --recursive
RUN ./autogen.sh
RUN ./configure && make && make install
RUN ldconfig

WORKDIR /
RUN mkdir -p .local
RUN wget -q -O cmake-linux.sh https://github.com/Kitware/CMake/releases/download/v3.17.0/cmake-3.17.0-Linux-x86_64.sh
RUN sh cmake-linux.sh -- --skip-license --prefix=.local
RUN rm cmake-linux.sh



RUN git clone --recurse-submodules -b v1.66.1 https://github.com/grpc/grpc

WORKDIR /grpc/third_party/cares/cares
RUN git fetch origin
RUN git checkout cares-1_13_0
RUN mkdir -p cmake/build
RUN cmake -DCMAKE_BUILD_TYPE=Release ./
RUN make -j4 install
RUN rm -rf third_party/cares/cares

WORKDIR /grpc/third_party/zlib
RUN mkdir -p cmake/build
RUN cmake -DCMAKE_BUILD_TYPE=Release ./
RUN make -j4 install
RUN rm -rf third_party/zlib

WORKDIR /grpc
RUN mkdir -p cmake/build
RUN cmake -DgRPC_INSTALL=ON -DgRPC_BUILD_TESTS=OFF -DCMAKE_INSTALL_PREFIX=./local ./
RUN make -j4 install

