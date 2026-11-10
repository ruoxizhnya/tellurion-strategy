# 0004 · core 消费边界与 promote 归属

- **状态**：已裁决（2026-11-10，主任）
- **背景**：`SVC-STRAT-G3` 原文要求 core 的解析/注册/promote 全部走本仓，并删除或只读本地注册表；但 core `SVC-CORE-STRAT-RETIRE`（2026-11-09 ✅）已删 `quant.strategies` 表并把二选一定死为**本地 registry（不改 HTTP）**，core `QT-32`（2026-11-10 ✅）的 K11 闸门也已生产接线。按原文验收与代码事实冲突。
- **决策**：
  1. **定义真相源 = 本仓**：资产注册、版本、构建、promote 审计归本仓；但 core **内置策略**的执行定义来自 core 本地 registry（代码内置），不联网、行为逐字节不变。
  2. core 只对**非空 `strategy_ref`** 调用本仓 `GET /api/strategies/{id}/resolve` 与 promotions 读端点，校验 `status=promoted` + 审计行；空 `strategy_ref` 跳过（内置路径）。
  3. **promote 由 agent/操作者直连本仓** `POST /api/strategies/{id}/versions/{hash}/promote`，带 service token + `X-Actor`；core **不提供** `/promote` 转发路由。
  4. 第三轮裁决 ⑫「promote 走 core 网关 + X-Actor」按**部署入口**解释（集群 ingress / 端口隔离），不是 core 代码级代理；若将来要求代码级网关，另立单。
  5. core `strategies` 表与 DB 资产注册腿已删；运行时 `pkg/strategy` 保留（ADR-0001）。
- **为什么不全量迁移（原文 G3）**：
  - 本仓无 list-all 端点，无法支撑 core `GET /api/strategies` 的全量清单；
  - 定义 schema（`carrier` / `version_hash` / `active_version_hash`）与 core `params` JSONB 语义不同（K5：不迁移）；
  - core 执行键是 `StrategyName`（本地 registry），改 HTTP 会引入启动依赖并破坏离线回测；
  - 恢复 core 注册/promote 代理等于反做已验收的 `SVC-CORE-STRAT-RETIRE`。
- **代价 / 后果**：promote 的「唯一入口」在部署层而非 core 代码层；审计权威在本仓 `strategy_promotions`；core 不保证执行前一定有人 promote，由 K11 resolve 闸门兜底。
- **否决的替代**：撤销本单（core 完全不接资产服务）会使 AI 回测闭环断、K11 失效；全量代理（本仓加 list-all + core 恢复注册/promote）与已落 RETIRE 冲突且破坏离线回测。
