CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS packages (
  name TEXT PRIMARY KEY,
  title TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  homepage TEXT NOT NULL DEFAULT '',
  aliases_json TEXT NOT NULL DEFAULT '[]',
  latest_version TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS package_aliases (
  alias TEXT PRIMARY KEY,
  package_name TEXT NOT NULL,
  FOREIGN KEY(package_name) REFERENCES packages(name) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS variants (
  package_name TEXT NOT NULL,
  variant_id TEXT NOT NULL,
  label TEXT NOT NULL DEFAULT '',
  os TEXT NOT NULL DEFAULT '',
  is_default INTEGER NOT NULL DEFAULT 0,
  default_flag INTEGER NOT NULL DEFAULT 0,
  version TEXT NOT NULL DEFAULT '',
  channel TEXT NOT NULL DEFAULT '',
  file_name TEXT NOT NULL DEFAULT '',
  download_url TEXT NOT NULL DEFAULT '',
  install_command TEXT NOT NULL DEFAULT '',
  sha256 TEXT NOT NULL DEFAULT '',
  install_strategy TEXT NOT NULL DEFAULT '',
  uninstall_strategy TEXT NOT NULL DEFAULT '',
  install_root TEXT NOT NULL DEFAULT '',
  binary_name TEXT NOT NULL DEFAULT '',
  wrapper_name TEXT NOT NULL DEFAULT '',
  launcher_path TEXT NOT NULL DEFAULT '',
  notes_json TEXT NOT NULL DEFAULT '[]',
  metadata_json TEXT NOT NULL DEFAULT '{}',
  source_json TEXT NOT NULL DEFAULT '{}',
  update_command TEXT NOT NULL DEFAULT '',
  update_policy TEXT NOT NULL DEFAULT '',
  auto_update INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (package_name, variant_id),
  FOREIGN KEY(package_name) REFERENCES packages(name) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS releases (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  package_name TEXT NOT NULL,
  variant_id TEXT NOT NULL,
  version TEXT NOT NULL,
  file_name TEXT NOT NULL,
  relative_path TEXT NOT NULL,
  content_type TEXT NOT NULL DEFAULT '',
  sha256 TEXT NOT NULL DEFAULT '',
  size INTEGER NOT NULL DEFAULT 0,
  published_at TEXT NOT NULL,
  UNIQUE(package_name, variant_id, version),
  FOREIGN KEY(package_name, variant_id) REFERENCES variants(package_name, variant_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS package_versions (
  package_name TEXT NOT NULL,
  version TEXT NOT NULL,
  readme TEXT NOT NULL DEFAULT '',
  notes TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (package_name, version),
  FOREIGN KEY(package_name) REFERENCES packages(name) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS package_dist_tags (
  package_name TEXT NOT NULL,
  tag TEXT NOT NULL,
  version TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (package_name, tag),
  FOREIGN KEY(package_name) REFERENCES packages(name) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_variants_package ON variants(package_name, variant_id);
CREATE INDEX IF NOT EXISTS idx_releases_package ON releases(package_name, version DESC, published_at DESC);
CREATE INDEX IF NOT EXISTS idx_releases_package_variant ON releases(package_name, variant_id, version DESC);
CREATE INDEX IF NOT EXISTS idx_package_versions_package ON package_versions(package_name, version DESC);
CREATE INDEX IF NOT EXISTS idx_package_dist_tags_package ON package_dist_tags(package_name, version);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (1);
INSERT OR IGNORE INTO schema_migrations(version) VALUES (2);
