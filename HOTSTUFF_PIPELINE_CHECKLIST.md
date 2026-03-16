# HotStuff 系列流水线化清单

本文档面向当前仓库中的 `hotstuff`、`fast-hotstuff`、`hpbft` 三条实现分支，目标是整理“从当前单轮 QC 闭环”演进到“流水线化（pipeline）”所需的开发清单。

当前代码现状：
- `hotstuff` 已实现最小链式流水线：parent/justifyQC、`highQC`、`lockedQC`、three-chain commit。
- `fast-hotstuff`、`hpbft` 仍属于单轮 `Proposal -> Vote -> QC` 的简化闭环。
- 仍未实现 pacemaker、view change、签名者集合、完整 block tree 恢复。

## 总目标

将当前实现改造成可以连续提出多个 block 的流水线共识：
- 新提案引用前一个 block/QC
- 每个 view 都可以继续推进链
- 提交规则不再是“拿到一个 QC 就结束”，而是依据链上连续 QC 决定 commit
- 允许 leader 在前一轮尚未最终提交时继续发下一轮 proposal

## 第一层：数据结构改造

- [x] 在 `internal/common/types.go` 中为提案消息增加区块链式字段：
  - `BlockID`
  - `ParentBlockID`
  - `JustifyQC`
  - `Proposer`
  - `Payload`
- [x] 定义本地 block 结构，而不是只存 `digest + tx`
- [x] 为每个高度或逻辑槽位维护 block 树/链状态
- [x] 增加 `lockedQC`
- [x] 增加 `highQC`
- [ ] 增加 `preferredView` 或等价状态

## 第二层：HotStuff 基础链式流程

- [x] proposal 必须携带父块引用
- [x] proposal 必须携带 `JustifyQC`
- [x] 副本在投票前校验：
  - 父块是否存在或可恢复
  - `JustifyQC` 是否合法
  - proposal 是否满足锁规则
- [x] leader 不能随意生成孤立块，必须围绕 `highQC` 延展

## 第三层：提交规则

### HotStuff

- [x] 实现三链提交规则（three-chain commit）：
  - block `b`
  - `b'` extends `b`
  - `b''` extends `b'`
  - 当形成连续 QC 链时提交 `b`
- [x] 将“收到一个 QC 就结束本高度”的逻辑改为“根据 QC 链提交祖先块”
- [x] 提交后执行 payload，并推进已提交高度

### Fast-HotStuff

- [ ] 明确 fast path 与 fallback path 的状态
- [ ] 区分普通 QC 和 commit QC
- [ ] 在快路径失败时回退到普通 HotStuff 提交流程
- [ ] 确保快路径不会破坏锁规则与安全性

### HPBFT

- [ ] 将 HPBFT 明确为“沿用 Fast-HotStuff 提交规则”的变体，而不是独立提交协议
- [ ] 增加组领导者（group leaders）角色定义：
  - 哪些节点充当组领导者
  - 分组规则如何确定
  - 组领导者与主 leader 的职责边界
- [ ] 增加组内投票收集流程：
  - 副本先向所属组领导者发送投票或签名份额
  - 组领导者做局部聚合或转发
  - 主 leader 汇总各组结果形成最终 QC
- [ ] 明确 HPBFT 与 Fast-HotStuff 的主要差异只落在：
  - QC 转发路径
  - 签名聚合的分层收集
  - 通信复杂度优化
- [ ] commit 条件保持与 Fast-HotStuff 一致，避免再引入一套单独的提交判定

## 第四层：投票与 QC 管理

- [x] 每个 block/view 只能投一次票
- [ ] 节点重启或重复消息场景下，不能对冲突 block 重复投票
- [x] leader 收集 `t` 个 vote 后生成 QC
- [ ] QC 必须绑定：
  - block ID
  - view
  - height 或 slot
  - 签名者集合或可验证的聚合信息
- [ ] 本地保存最近若干个 QC，供 proposal 和 view change 使用

## 第五层：流水线窗口

- [ ] 定义“可并行推进的未提交 block 数量”
- [ ] 允许 leader 在前一个 block 未 commit 时继续 proposal 下一个 block
- [ ] 为节点增加：
  - `nextProposalView`
  - `highestCommittedView`
  - `highestVotedView`
- [ ] 限制 leader 不要无限制堆积未提交 block
- [ ] 设计窗口大小：
  - 固定窗口
  - 或根据提交进度滑动

## 第六层：Pacemaker 与 View Change

- [ ] 为 HotStuff 系列统一增加 pacemaker
- [ ] 增加超时触发：
  - leader 未按时 proposal
  - proposal 未按时形成 QC
- [ ] 增加 new view 消息
- [ ] 新 leader 应从收到的状态中选择最高 `highQC`
- [ ] 新 leader 使用 `highQC` 继续延展链，而不是重新起一条无依据的链
- [ ] 超时切换不应丢失已形成的 QC 信息

## 第七层：执行与提交分离

- [x] 将“收到 proposal 就执行负载”的逻辑改为“提交后再执行”或至少区分：
  - speculative execute
  - committed execute
- [ ] 若仍保留预执行，必须在文档中明确说明这是性能模拟，不是严格语义
- [x] 客户端 `/end` 不应在拿到单个 QC 后立刻返回，而应在满足 commit 规则后返回

## 第八层：消息与接口

- [x] 在 `ConsensusMessage` 中显式区分：
  - `Proposal`
  - `Vote`
  - `QC`
  - `NewView`
- [x] proposal 应包含 block 元数据，而不是仅有 `digest` 和 `tx`
- [ ] QC 最好有独立结构，而不是继续复用一个字符串字段
- [ ] 如果继续使用当前简化签名，需要至少把签名者集合带上

## 第九层：存储与恢复

- [x] 将区块保存迁移到独立目录，不再只依赖内存状态
- [x] 每个节点使用自己的本地数据库
- [x] 优先采用 `LevelDB`
- [x] 保存 block tree
- [x] 保存 `highQC`
- [x] 保存 `lockedQC`
- [x] 保存已投票视图
- [x] 保存已提交链头
- [ ] 保存组领导者的中间聚合结果（用于 HPBFT）
- [ ] 节点重启后应能恢复未完成的流水线状态

## 第十层：当前代码对应改动点

- [ ] `internal/common/types.go`
  - 增加 block/QC/new-view 结构
- [ ] `internal/nodesvc/service.go`
  - 将 `processOneVote` 拆成更细的链式处理流程
  - 增加 block tree 管理
  - 增加 commit 规则判断
  - 增加 pacemaker/view change
- [ ] `internal/crypto/crypto.go`
  - 若后续引入 BLS，需要为 QC 验证提供签名者信息
- [x] `internal/storage/`
  - 增加每节点独立的 LevelDB 存储层
- [x] `internal/clientsvc/service.go`
  - 将 `/end` 触发时机调整为真正 commit 之后
- [x] `ALGORITHMS.md`
  - 更新为流水线版流程说明

## 实现优先级建议

### Phase 1：先把 HotStuff 做对

- [x] block 结构
- [x] parent + justifyQC
- [x] `highQC` / `lockedQC`
- [x] three-chain commit
- [x] commit 后再 `/end`

### Phase 2：再做 view change

- [ ] pacemaker
- [ ] timeout
- [ ] new view
- [ ] 恢复最高 `highQC`

### Phase 3：再扩展到 Fast-HotStuff 与 HPBFT

- [ ] Fast-HotStuff 的 fast path / fallback
- [ ] HPBFT 的组领导者转发与分组聚合
- [ ] 公共逻辑抽取

## 算法流程建议

### 1. Chained HotStuff

推荐的流水线流程：
1. leader 选择当前 `highQC` 对应的 block 作为父块
2. leader 构造新 block，并在 proposal 中附带 `JustifyQC`
3. 副本验证 proposal 是否扩展自合法链，并检查锁规则
4. 合法则对新 block 投票
5. leader 聚合投票生成新 QC
6. 新 QC 成为新的 `highQC`
7. 下一视图继续围绕这个 `highQC` 发 proposal
8. 当形成连续三段 QC 链时，提交最老的祖先块

### 2. Fast-HotStuff

推荐实现顺序：
1. 先完整继承 chained HotStuff 的基础结构
2. 在此之上增加 fast path
3. 满足快路径条件时生成 `CommitQC`
4. 不满足时自动退回普通 HotStuff 路径

### 3. HPBFT

推荐做法：
1. 直接沿用 `Fast-HotStuff` 的流水线与提交规则
2. 将差异限定在 QC 收集拓扑：
   - 普通副本不一定直接把 vote 发给主 leader
   - 先发给组领导者
   - 组领导者负责转发、局部聚合或分组收集
   - 主 leader 再汇总形成最终 `CommitQC`
3. 这样 `HPBFT` 的核心语义是“分层收集的 Fast-HotStuff”，不是独立的提交协议

## 验证清单

- [ ] leader 正常工作时，链能持续增长
- [ ] 未 commit 前可以继续 proposal 新 block
- [ ] 节点不会为冲突链重复投票
- [ ] leader 故障后，新 leader 能从最高 `highQC` 恢复
- [ ] 旧 leader 恢复后发送旧 proposal，不会破坏安全性
- [ ] 只有满足提交规则的 block 才会触发 `/end`
- [ ] 长链运行时不会无限积压未提交 block

## 你现在最应该先做的部分

如果目标是“先把 HotStuff 系列做成流水线”，最先落地的最小闭环应该是：
1. 只改 `hotstuff`
2. 引入 block 链、`highQC`、`lockedQC`
3. 实现 three-chain commit
4. 把 `/end` 从“拿到 QC”改成“真正 commit”
5. 跑通后再去扩展 `fast-hotstuff` 和 `hpbft`

这是因为当前 `fast-hotstuff` 和 `hpbft` 都依赖你先有一个正确的链式 HotStuff 骨架。其中 `hpbft` 的增量不在提交规则，而在“组领导者参与 QC 转发与签名聚合的分层收集”。
