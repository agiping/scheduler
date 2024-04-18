package policy

/***

curl 127.0.0.1:80/generate  -X POST -d '
{
   "struct_input": {
      "session_id": "123", #可以不填写，标记不进行session力度的cache
      "sys_prompt": { #可以不存在，标记不进行sys_prompt sys_prompt cache
         "id": "", #标记cache的唯一id，可以不填写，会将input md5
         "input": "long long long PE"
      }
      "history": [
         {
            "id": "", #标记cache的唯一id，可以不填写，会将input md5
            "input": "<H_Q>请问你是谁？<H_A>",
            "output": "我是助手", #output 可以不存在
         },
      ],
      "cur": {
         "id": "", #标记cache的唯一id，可以不填写，会将input md5
         "input": "<C_Q>今日天气如何？<C_A>"
      }
   },
   "inputs":"", #此处必须存在且为空
   "parameters":{
      "repetition_penalty":1.0,
      "temperature":0.1,
      "top_k":1,
      "top_p":0.85,
      "max_new_tokens":2048,
      "do_sample":false,
      "seed":2023,
      "details":true
   }
}' -H 'Content-Type: application/json'

***/

/***
```pseudo

parameters:
===================
    Q_high: tgi_queue_size 的高负载阈值, e.g., 8;
  2*Q_high: 极限阈值
     Q_low: tgi_queue_size 的低负载阈值, e.g., 0;
         K: cache replication 的调整间隔
===================


Notions:
===================
            r: request, 分有状态、无状态两类
   session_id: 标记，是否需要 session cache
     all_pods: 两次扩缩容动作之间的所有实例
  p_stateless: 能处理无状态请求的pod集合
          nor: Number Of Requests of pods
       podSet: map{session_id: set(pod)}, 从session_id到pod集合的映射
            Ø: empty set
s, n, m, v, p: ID of a selected pod, i.e., pod ip
   autoscaler: 自动扩缩容模块
   ∃ instance: 存在一个满足...条件的实例
      lastMod: last modified timestamp, 上次修改时间戳
          |a|: 集合A的元素个数
===================

Algorithm:
// 初始, 所有 pod 都能处理无状态请求
// 且用 p_stateless 表示能处理无状态请求的pod集合
for s in all_pods:
    s.accept_stateless ← true
    add s to p_stateless

while (true)
    received request r;
    // 无状态请求走普通负载均衡
    if not r.session_id then
        if p_stateless == Ø then
            // 所有实例负载过高, 全局选择最小请求数实例
            // 并主动触发一次扩容操作，扩容事件会在 autoscaler 中排队，冷却期内可能会被取消
            autoscaler ← scale up event
            n ← {least nor instance in all_pods}
        else
            // 在所有可以处理无状态请求的实例中，选择最小请求数实例
            n ← {least nor instance in p_stateless}
    else:
        // 有状态请求走亲和性路由
        if podSet[r.session_id] == Ø then
            // 开启 session_cache 的首个请求
            n, podSet[r.session_id] ← {least nor instance in p_stateless};
        else
            n ← {least nor instance in podSet[r.session_id]};
            m ← {most nor instance in podSet[r.session_id]};
            // 负载均衡判断可采用 nor, 因为其绝对值不重要，看的是相对值
            // 而决定 session cache 是否需要在新的实例中重建，必须依赖实例的真实负载，需要绝对值
            // 因此，采用 tgi_queue_size
            if n.tgi_queue_size ≥ 2*Q_high then
                // 当前实例组负载过高，关闭无状态处理，最大限度保证有状态请求的缓存命中
                // 提高显存利用率
                for v in podSet[r.session_id] do
                    v.accept_stateless ← false
                    remove v from p_stateless
            // podSet[r.session_id]对应的实例负载较大，进行 re-balance
            // 此时 cache 会在新实例 p 中重建
            else if (n.tgi_queue_size > Q_high &&
                    ∃ instance in p_stateless with tgi_queue_size <= Q_low) then
                p ← {least nor instance p_stateless};
                add p to podSet[r.session_id];
                n ← p;
        // 我们需要避免同一个 session_id 的缓存数据占用过多实例,
        // 因此，每隔 K 秒，对该 session_id 的缓存组进行收缩，避免其cache重建过多份
        if |podSet[r.session_id]| > 1 &&
        time() - podSet[r.session_id].lastMod > K then
            // 从 podSet[r.session_id] 中移除 m 之后，短期内 m 不会再接收 r.session_id 的请求，
            // 由 tgi LRU 进行缓存释放
            remove m from podSet[r.session_id];
        if podSet[r.session_id] changed in this iteration then
            podSet[r.session_id].lastMod ← time();
    send r to n
    n.number_of_requests ++
    // 异步释放处理完毕的请求
    async:
      if n.responsed then
        n.number_of_requests --
        // 无状态恢复检查,
        // 若压力已回落，则开启无状态处理, 并加回 p_stateless
        if not n.accept_stateless && n.tgi_queue_size < Q_high:
            n.accept_stateless ← true
            add n to p_stateless

ASYNC PROCESS II: TGI Metrics
// 如果是首包返回模式, 有请求才会更新scheduler端的 tgi_queue_size;
// 没有新请求则无法更新, 但 tgi_queue_size 的实际分布会因请求执行结束而改变。
// 为了使 TGI 一侧的改动最小，由 scheduler 定期拉取 queue_size 指标
while (true)
    pull tgi_queue_size from tgi "/metrics" router

ASYNC PROCESS II: AutoScaler
// 根据 tgi_queue_size 扩容
// 缩容需要根据副本平均qps, average tgi_queue_size, average nor.
while (true)
    if received scale up event ← scheduler ||
       average(tgi_queue_size) ≥ Q_UP for C seconds then
        scale up

// 测试指标:
// 有状态请求在cache命中的时候，为热启动，否则为冷启动，无状态请求为冷启动；
// cache hit rate: 所有有状态请求中，成功匹配到cache的请求比例, 热启动比例
// latency, throughput

// 改进点：
对于给定 session_id, 假设请求序列为 [Q1, A1, Q2, A2, Q3, A3, Q4, A4, Q5, A5],
若 [Q1, A1, Q2, A2] 在 Pod1 执行，
   [Q3, A3] 在 Pod2 执行，
   [Q4, A4, Q5, A5] 又在 Pod1 执行，
则对于 [Q4, A4, Q5, A5] 的执行，[Q1, A1, Q2, A2] 依然是热启动 (Pod1上该session_id的缓存未过期时)，
KV Cache 会复用；[Q3, A3, Q4, A4, Q5, A5] 则冷启动；
***/
