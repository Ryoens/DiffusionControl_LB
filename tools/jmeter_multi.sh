#!/bin/bash

# ----------------------
# ユーザー定義パラメータ
# ----------------------
MAIN_URL="${1}"                   # 1番目の引数: フラッシュクラウド対象LBのURL
DURATION_SEC="${2:-60}"            # 2番目の引数: 負荷時間（秒）
MAIN_THREADS="${3:-50}"            # 3番目の引数: フラッシュクラウド対象LBの同時接続数
NUM_CLUSTER="${4:-20}"

all_targets=()
count=0
for count in $(seq 0 "$NUM_CLUSTER");
do 
  all_targets+=("\"http://172.18.4.$((count+2)):8001\"")
done

echo ${all_targets[*]}

# ----------------------
# jmxテンプレートの自動生成
# ----------------------
echo "Creating temp_test.jmx ..."
cat <<EOF > temp_test.jmx
<?xml version="1.0" encoding="UTF-8"?>
<jmeterTestPlan version="1.2" properties="5.0" jmeter="5.6.3">
  <hashTree>
    <TestPlan guiclass="TestPlanGui" testclass="TestPlan" testname="MultiTargetTestPlan" enabled="true">
      <stringProp name="TestPlan.comments"></stringProp>
      <boolProp name="TestPlan.functional_mode">false</boolProp>
      <boolProp name="TestPlan.serialize_threadgroups">false</boolProp>
      <elementProp name="TestPlan.user_defined_variables" elementType="Arguments">
        <collectionProp name="Arguments.arguments"/>
      </elementProp>
      <stringProp name="TestPlan.user_define_classpath"></stringProp>
    </TestPlan>
    <hashTree>
EOF

# ----------------------
# 各ターゲット用のThreadGroupを生成
# ----------------------
for target in "${all_targets[@]}"; do
    ip=$(echo "$target" | sed -E 's~https?://([^:/]+).*~\1~')
    port=$(echo "$target" | grep -oP ':(\d+)' | tr -d ':')
    clean_target=$(echo "$target" | tr -d '"' | sed 's:/$::')

    if [ "$clean_target" = "$MAIN_URL" ]; then
        # フラッシュクラウド対象LB
        threads=$MAIN_THREADS
    else
        # その他LB
        threads=$(( MAIN_THREADS / 10 ))
    fi

cat <<EOF >> temp_test.jmx
      <ThreadGroup guiclass="ThreadGroupGui" testclass="ThreadGroup" testname="Target-$ip" enabled="true">
        <stringProp name="ThreadGroup.on_sample_error">continue</stringProp>
        <elementProp name="ThreadGroup.main_controller" elementType="LoopController">
          <boolProp name="LoopController.continue_forever">false</boolProp>
          <intProp name="LoopController.loops">-1</intProp>
        </elementProp>
        <stringProp name="ThreadGroup.num_threads">${threads}</stringProp>
        <stringProp name="ThreadGroup.ramp_time">1</stringProp>
        <boolProp name="ThreadGroup.scheduler">true</boolProp>
        <stringProp name="ThreadGroup.duration">${DURATION_SEC}</stringProp>
        <stringProp name="ThreadGroup.delay">0</stringProp>
      </ThreadGroup>
      <hashTree>
        <HTTPSamplerProxy guiclass="HttpTestSampleGui" testclass="HTTPSamplerProxy" testname="HTTP Request to $ip" enabled="true">
          <elementProp name="HTTPsampler.Arguments" elementType="Arguments">
            <collectionProp name="Arguments.arguments"/>
          </elementProp>
          <stringProp name="HTTPSampler.domain">${ip}</stringProp>
          <stringProp name="HTTPSampler.port">${port}</stringProp>
          <stringProp name="HTTPSampler.protocol">http</stringProp>
          <stringProp name="HTTPSampler.path">/</stringProp>
          <stringProp name="HTTPSampler.method">GET</stringProp>
          <boolProp name="HTTPSampler.follow_redirects">true</boolProp>
          <boolProp name="HTTPSampler.auto_redirects">false</boolProp>
          <boolProp name="HTTPSampler.use_keepalive">true</boolProp>
          <boolProp name="HTTPSampler.DO_MULTIPART_POST">false</boolProp>
        </HTTPSamplerProxy>
        <hashTree/>
      </hashTree>
EOF

done

# ----------------------
# JMXファイル閉じる
# ----------------------
cat <<EOF >> temp_test.jmx
    </hashTree>
  </hashTree>
</jmeterTestPlan>
EOF

# ----------------------
# JMeter テスト実行
# ----------------------
echo "Running JMeter test..."
jmeter -n -t temp_test.jmx -l ../log/result_${DURATION_SEC}s.jtl -j ../log/jmeter.log -Jxstream.security.allow=com.thoughtworks.xstream.security.AnyTypePermission
