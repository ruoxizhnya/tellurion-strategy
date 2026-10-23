package asset

// DDL 是本仓对 strategy schema 的唯一建表语句（迁移机制按 docs/db-schema.md §4
// 取向 ①：Go 数组 + 启动执行，幂等、无版本表）。
//
// 纪律（与 core 的 migrationStatements 同规则）：
//   - 只前滚、每条可重入（IF NOT EXISTS），空库一次跑完；
//   - 已落地语句不改，修错靠追加一条；
//   - 结构变了本文件与 docs/db-schema.md 必须同一次提交。
//
// 两处主键裁决（2026-10-23，docs/adr/0002）：
//   - strategy_artifacts 主键是 (strategy_id, artifact_hash)，不是
//     (strategy_id, version_hash)——同源码 × 两工具链必须两行（QT-41b ③-③）；
//   - definition 补 active_artifact_hash 第 7 列（对表偏差 #1），否则「当前生效
//     的是哪个字节」在两工具链下无法回答。

// Migrations 返回建表语句副本（调用方拿到的是拷贝，改不动本包常量）。
func Migrations() []string {
	out := make([]string, len(migrationStatements))
	copy(out, migrationStatements)
	return out
}

var migrationStatements = []string{
	`CREATE SCHEMA IF NOT EXISTS strategy`,

	`CREATE TABLE IF NOT EXISTS strategy.strategy_definitions (
		id                   TEXT PRIMARY KEY,
		carrier              TEXT NOT NULL CHECK (carrier IN ('builtin','dsl','wasm','so','external')),
		abi_version          TEXT NOT NULL DEFAULT '',
		scope                TEXT NOT NULL CHECK (scope IN ('market','symbol')),
		capability           JSONB NOT NULL DEFAULT '{}'::jsonb,
		entry                TEXT NOT NULL,
		active_version_hash  CHAR(64),
		active_artifact_hash CHAR(64),
		created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
		updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
		CHECK ((active_version_hash IS NULL) = (active_artifact_hash IS NULL))
	)`,

	`CREATE TABLE IF NOT EXISTS strategy.strategy_versions (
		strategy_id  TEXT NOT NULL REFERENCES strategy.strategy_definitions(id) ON DELETE CASCADE,
		version_hash CHAR(64) NOT NULL,
		source       TEXT NOT NULL,
		carrier      TEXT NOT NULL,
		abi_version  TEXT NOT NULL DEFAULT '',
		scope        TEXT NOT NULL,
		entry        TEXT NOT NULL,
		created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
		PRIMARY KEY (strategy_id, version_hash)
	)`,

	`CREATE TABLE IF NOT EXISTS strategy.strategy_artifacts (
		strategy_id        TEXT NOT NULL,
		artifact_hash      CHAR(64) NOT NULL,
		version_hash       CHAR(64) NOT NULL,
		carrier            TEXT NOT NULL,
		abi_version        TEXT NOT NULL,
		toolchain          TEXT NOT NULL,
		size_bytes         BIGINT NOT NULL CHECK (size_bytes >= 0),
		memory_limit_bytes BIGINT NOT NULL CHECK (memory_limit_bytes > 0),
		imports            TEXT[] NOT NULL DEFAULT '{}',
		status             TEXT NOT NULL CHECK (status IN ('built','validated','promoted','rejected','deprecated')),
		storage            TEXT NOT NULL DEFAULT 'inline' CHECK (storage IN ('inline','url')),
		compile_log        TEXT NOT NULL DEFAULT '',
		built_by           TEXT NOT NULL DEFAULT '',
		created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
		PRIMARY KEY (strategy_id, artifact_hash),
		UNIQUE (strategy_id, version_hash, toolchain),
		FOREIGN KEY (strategy_id, version_hash)
			REFERENCES strategy.strategy_versions(strategy_id, version_hash) ON DELETE CASCADE
	)`,

	`CREATE TABLE IF NOT EXISTS strategy.strategy_artifact_blobs (
		strategy_id   TEXT NOT NULL,
		artifact_hash CHAR(64) NOT NULL,
		bytes         BYTEA NOT NULL CHECK (octet_length(bytes) <= 8388608),
		PRIMARY KEY (strategy_id, artifact_hash),
		FOREIGN KEY (strategy_id, artifact_hash)
			REFERENCES strategy.strategy_artifacts(strategy_id, artifact_hash) ON DELETE CASCADE
	)`,
}
