# 0002 · artifact 主键与生效字节的裁决

- **状态**：已裁决（2026-10-23，QT-41 首批实装前置）
- **场景**：`strategy-asset-service-api.md` 写的是「`strategies` 加 6 个新列」，但 `context` 与 `promote` 语义里一直用
  `active_artifact_hash`；同源码 × 两工具链（QT-41b ③-③）也要求 artifact 行能分别追溯。开工对表时逮出两条前提偏差
  （`docs/api-schema.md` §6.5 偏差 #1）：
  1. 新列到底是 6 个还是 7 个；
  2. `strategy_artifacts` 的主键该是 `(strategy_id, artifact_hash)` 还是 `(strategy_id, version_hash, toolchain)`。
- **决策**：
  1. **定义表补第 7 列 `active_artifact_hash`**，并用
     `CHECK ((active_version_hash IS NULL) = (active_artifact_hash IS NULL))` 钉住「要么都没有、要么都有」。
  2. **artifact 主键 = `PRIMARY KEY (strategy_id, artifact_hash)`**，另加
     `UNIQUE (strategy_id, version_hash, toolchain)`。
- **理由**：
  - `resolve` 要能不查 artifact 行就回答「此刻生效的是哪份字节」。只靠 `status='promoted'` 反查，在两工具链场景下不唯一
    （同版本可能两行都 promoted），会把「命中哪一行」变成实现细节。
  - 内容寻址优先：**字节相同即同一个 artifact**。同字节跨工具链不产生第二行——否则同一份字节会有两条互相矛盾的
    「编译来源」，`artifact_hash` 作为缓存键的语义就漏了。
  - 同源码 × 两工具链 → 字节不同 → 天然是两行，`UNIQUE(strategy_id, version_hash, toolchain)` 保证同版本同工具链
    不允许第二份字节（那是"同一编法产出两个不同结果"，属实现异常，必须 fail-loud）。
- **代价 / 后果**：
  - 同一份字节只能记一个 `toolchain`：后到的工具链若产出**相同**字节，登记会以 `artifact_hash` 冲突拒收（`conflict`）。
    这是有意的——「谁编译的」不允许多真相。测试已钉住（`internal/httpapi` 的 `RegisterArtifactFailsClosed`）。
  - `active_artifact_hash` 是**单指针**：同版本另一工具链后来 promote 会覆盖它。「多内核各持一个 active」取决于
    NT 侧接入形态（未决 C），本 ADR 不预设。
  - 接 PG 后 `Promote` 需事务：定义指针与 artifact 状态是两次写（当前 `Memory` 单锁内无此问题，已记为 QT-41 未落项 10）。
- **替代关系**：无（首条针对表结构与主键的 ADR）。源文 `tellurion:docs/design/qt-decoupling/strategy-asset-service-api.md`
  的「6 个新列」表述**被本 ADR 修订为 7 列**；落地形态见 `docs/db-schema.md` §1/§5 与 `internal/asset/ddl.go`。
