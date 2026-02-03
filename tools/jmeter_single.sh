#!/bin/bash

# ----------------------
# User-defined parameters
# ----------------------
TARGET_URL="${1:-http://localhost:8080}"     # 1st argument: Target URL (default is localhost)
DURATION_SEC="${2:-60}"                      # 2nd argument: Load duration (seconds)
THREADS="${3:-50}"                           # 3rd argument: Number of concurrent users

# ----------------------
# Automatic generation of jmx template
# ----------------------
cat <<EOF > temp_test.jmx
<?xml version="1.0" encoding="UTF-8"?>
<jmeterTestPlan version="1.2" properties="5.0" jmeter="5.5">
  <hashTree>
    <TestPlan guiclass="TestPlanGui" testclass="TestPlan" testname="AutoTestPlan" enabled="true">
      <stringProp name="TestPlan.comments"></stringProp>
      <boolProp name="TestPlan.functional_mode">false</boolProp>
      <boolProp name="TestPlan.serialize_threadgroups">false</boolProp>
      <elementProp name="TestPlan.user_defined_variables" elementType="Arguments" guiclass="ArgumentsPanel" testclass="Arguments" enabled="true">
        <collectionProp name="Arguments.arguments"/>
      </elementProp>
      <stringProp name="TestPlan.user_define_classpath"></stringProp>
    </TestPlan>
    <hashTree>
      <ThreadGroup guiclass="ThreadGroupGui" testclass="ThreadGroup" testname="AutoThreadGroup" enabled="true">
        <stringProp name="ThreadGroup.on_sample_error">continue</stringProp>
        <elementProp name="ThreadGroup.main_controller" elementType="LoopController" guiclass="LoopControlPanel" testclass="LoopController" enabled="true">
          <boolProp name="LoopController.continue_forever">false</boolProp>
          <intProp name="LoopController.loops">-1</intProp>
        </elementProp>
        <stringProp name="ThreadGroup.num_threads">${THREADS}</stringProp>
        <stringProp name="ThreadGroup.ramp_time">1</stringProp>
        <boolProp name="ThreadGroup.scheduler">true</boolProp>
        <stringProp name="ThreadGroup.duration">${DURATION_SEC}</stringProp>
        <stringProp name="ThreadGroup.delay">0</stringProp>
      </ThreadGroup>
      <hashTree>
        <HTTPSamplerProxy guiclass="HttpTestSampleGui" testclass="HTTPSamplerProxy" testname="HTTP Request" enabled="true">
          <elementProp name="HTTPsampler.Arguments" elementType="Arguments">
            <collectionProp name="Arguments.arguments"/>
          </elementProp>
          <stringProp name="HTTPSampler.domain">$(echo "$TARGET_URL" | sed -E 's~https?://([^:/]+).*~\1~')</stringProp>
          <stringProp name="HTTPSampler.port">$(echo "$TARGET_URL" | grep -oP ':(\d+)' | tr -d ':')</stringProp>
          <stringProp name="HTTPSampler.protocol">$(echo "$TARGET_URL" | grep -oE '^https?')</stringProp>
          <stringProp name="HTTPSampler.path">/</stringProp>
          <stringProp name="HTTPSampler.method">GET</stringProp>
          <boolProp name="HTTPSampler.follow_redirects">true</boolProp>
          <boolProp name="HTTPSampler.auto_redirects">false</boolProp>
          <boolProp name="HTTPSampler.use_keepalive">true</boolProp>
          <boolProp name="HTTPSampler.DO_MULTIPART_POST">false</boolProp>
        </HTTPSamplerProxy>
        <hashTree/>
      </hashTree>
    </hashTree>
  </hashTree>
</jmeterTestPlan>
EOF

# ----------------------
# Run JMeter test
# ----------------------
echo "Running JMeter test against $TARGET_URL for ${DURATION_SEC}s with ${THREADS} threads..."
jmeter -n -t temp_test.jmx -l ../log/result_${DURATION_SEC}s.jtl -j ../log/jmeter.log -Jxstream.security.allow=com.thoughtworks.xstream.security.AnyTypePermission