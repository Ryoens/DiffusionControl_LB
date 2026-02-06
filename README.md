# Load Balancing Method based on Diffusion Control

## 概要
NW上の拡散方程式に基づいてノード間で自律分散的に動作する負荷分散方式
- NW上に複数のLBが無向グラフで形成
- gRPCによるフィードバック情報の取得
    - 隣接LB間で以下の制御情報を送受信
        - セッション数
    - サーバ: 現在のセッション数を送信
    - クライアント: 隣接LBからセッション数を受信
- 転送するリクエスト数
    - 拡散係数 × (自身のセッション数 - 隣接のセッション数)
- 隣接LBへのリクエスト転送可否を閾値により判断

## ファイル構成

```
.
├── api                                    
│   ├── hello.pb.go       
│   ├── hello.proto       
│   └── hello_grpc.pb.go  
├── cmd                   
│   ├── DockerBuild.sh    
│   ├── DockerDestroy.sh  
│   ├── Dockerfile        
│   ├── Execute.sh        
│   ├── ExecuteRange.sh  
│   └── ExecuteWebUI.sh   
├── lb 
│   ├── lb_diff.go 
│   ├── lb_lc.go 
│   ├── lb_new.go 
│   ├── lb_rr.go 
│   └── lb_thre.go 
├── prometheus         
│   └── federation
│       └── prometheus.yml 
├── tools              
|   ├── adjacentListController.py 
|   ├── delayController.py        
|   ├── delbr.sh                  
|   ├── jmeter_multi.sh           
|   ├── jmeter_result_extraction.sh 
|   ├── jmeter_single.sh          
|   ├── to_average.py             
|   └── to_median.py              
├── go.mod             
├── go.sum                
├── Makefile              
├── README.md                         
```

## 実行方法

### 環境構築

```bash
apt install curl golang-go protobuf-compiler jmeter make python3-pip
```

### コンテナ構築
`cmd/DockerBuild.sh`を実行

`make build [クラスタ数]`

1. 各クラスタのWebサーバ数を指定
2. Dockerイメージ`LB`が存在しなければ作成
3. Dockerネットワーク`overlay-net`を作成
4. Redisコンテナを作成
5. 指定した`クラスタ数`を元にDockerコンテナを構築
    - クラスタごとの`compose.yml`が`cmd/`に自動で作成
    - `json/config.json`に各クラスタの情報が追記
        - LB: `"cluster_lb": "[IP address]"`
        - Web: `"web*": "[IP address]"`

### プログラムの実行
`cmd/Execute.sh`を実行

※ 遅延設定は未導入

`make exec`

1. 実験パラメータの設定
    - フィードバック間隔、閾値、拡散係数を入力
    - 同時接続数、試行回数を指定
    - ネットワークモデル(fullmesh/random/barabasi-albert)を選択
2. 選択したネットワークモデルを元に隣接リスト(adjacentList)を作成
    - `tools/adjacentListController.py`でクラスタ間の接続関係を生成
    - `json/config.json`から`json/adjacentList.json`が生成
    - 生成した接続関係は`data/figure_*.png`に出力
3. フラッシュクラウドを発生させるクラスタを指定
4. 適用する負荷分散アルゴリズムの選択
    - DC(threshold-based: `lb_thre.go`)/DC(difference-based: `lb_diff.go`)/RR(`lb_rr.go`)/LC(`lb_lc.go`)から選択
5. LBプログラムのビルド
    - 各クラスタのLBコンテナ内でコンパイル
    - コンパイルしたプログラムは`compiled/`配下に出力
6. LBプログラムの実行
    - 全クラスタのLBを起動し、Redis経由で準備完了を確認
7. 負荷テストの実行
    - JMeterで指定した同時接続数, 時間で負荷試験
    - `tools/jmeter_multi.sh`が実行
8. データ収集
    - 各クラスタのLBからメトリクスデータ(csv形式)をcurlで取得
    - JMeterのログとリザルトファイルを保存
9. データ処理
    - 平均値, 中央値の計算
        - 実験結果は`data/`配下に出力
    - 実験パラメータの記録

### コンテナ削除
`cmd/DockerDestroy.sh`を実行

`make destroy [コンテナ数]`

### トラブルシューティング

## License
MIT