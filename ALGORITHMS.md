# 共识流程说明（按实现）

本文档描述当前代码中各算法的实际运行流程（为最小可运行的简化闭环实现）。

## 共通机制

- **Leader 选择**
  - 所有算法：`leader = ((view-1) % N) + 1`。
- **高度与视图**
  - 初始 `height=1`、`view=1`，每完成一轮后 `height++` 且 `view=height`。
- **消息去重**
  - 基于 `view/height/digest/from/type` 生成去重键，重复消息直接丢弃。
- **起止时延**
  - leader 在发起提案时向 client 发送 `/start`。
  - 节点完成本轮后向 client 发送 `/end`。
  - client 收到 `q=floor(N/3)+1` 个去重 `/end` 后打印延迟。

## SBFT（简化 collector 流程）

**消息类型与路由**  
`PrePrepare` / `Prepare` / `CommitProof` 通过 `/sbft/message` 发送。

**流程**
1. **Leader 提案**  
   - 生成交易负载，计算 `digest`。  
   - 调用 `/start` 记录起始时间。  
   - 广播 `PrePrepare`。
2. **副本返回 Prepare**  
   - 校验 `digest` 是否匹配。  
   - 执行负载模拟。  
   - 生成 `Prepare` 并发送给 leader。
3. **Leader 聚合提交证明**  
   - 验证签名份额。  
   - 当 `Prepare` 数量达到 `t=floor(2N/3)+1`，聚合生成 `CommitProof`。  
   - 广播提交证明，并上报 `/end`，推进到下一高度。
4. **节点完成**  
   - 收到 `CommitProof` 即视为本轮完成，上报 `/end`，推进到下一高度。

## HotStuff（链式流水线）

**消息类型与路由**  
`HSProposal` / `HSVote` / `HSQC` 通过 `/hotstuff/message` 发送。

**流程**
1. **Leader 提案**  
   - 选择当前 `highQC` 认证的父块。  
   - 生成新区块并附带 `ParentID`、`JustifyQC`、`JustifyView`。  
   - 调用 `/start`，广播 `HSProposal`。
2. **节点投票**  
   - 校验父块存在、`JustifyQC` 合法且满足锁规则。  
   - 生成 `HSVote` 并发送给 leader。
3. **Leader 聚合 QC**  
   - 收集到 `t` 个 `HSVote` 后聚合为 `HSQC`。  
   - 将该 QC 更新为新的 `highQC`，并广播 `HSQC`。  
   - 下一视图继续围绕新的 `highQC` 发 proposal。
4. **三链提交**  
   - 当形成连续三段祖先链 `b <- b' <- b''` 且当前为 `QC(b'')` 时，提交最老祖先块 `b`。  
   - 负载执行与 `/end` 上报在提交时发生，而不是在拿到单个 QC 时发生。

## Fast-HotStuff（单轮提案-投票-CommitQC）

**消息类型与路由**  
`FHSProposal` / `FHSVote` / `FHSCommitQC` 通过 `/fast-hotstuff/message` 发送。

**流程**
1. **Leader 提案**：与 HotStuff 相同，发送 `FHSProposal`。  
2. **节点投票**：校验后发送 `FHSVote` 给 leader。  
3. **Leader 生成 CommitQC**：收集到 `t` 个投票后广播 `FHSCommitQC`。  
4. **节点完成**：收到 `FHSCommitQC` 即完成，上报 `/end` 并推进高度。

## HPBFT（当前实现为单轮提案-投票-QC，目标语义为分层收集的 Fast-HotStuff）

**消息类型与路由**  
`HPProposal` / `HPPrepareVote` / `HPQC` 通过 `/hpbft/message` 发送。

**流程**
1. **Leader 提案**：生成负载，发送 `HPProposal`。  
2. **节点投票**：校验后发送 `HPPrepareVote` 给 leader。  
3. **Leader 生成 QC**：收集到 `t` 个投票后广播 `HPQC`。  
4. **节点完成**：收到 `HPQC` 即完成，上报 `/end` 并推进高度。

**后续目标**
- HPBFT 的提交规则沿用 `Fast-HotStuff`。
- 与 `Fast-HotStuff` 的主要区别不在 commit 条件，而在通信拓扑：
  - 引入组领导者参与 QC 转发。
  - 签名份额先分组收集，再由主 leader 汇总聚合。

## 说明与简化点

- 这些流程是 **教学/演示级的简化闭环**，并非完整协议实现。  
- “签名”和“聚合”当前为演示逻辑（HMAC + 排序哈希），不是 BLS。  
- 未实现超时、视图切换、重试、持久化日志等完整 SBFT/HotStuff 机制。  
