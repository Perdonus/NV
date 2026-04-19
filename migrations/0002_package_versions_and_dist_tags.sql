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

CREATE INDEX IF NOT EXISTS idx_package_versions_package ON package_versions(package_name, version DESC);
CREATE INDEX IF NOT EXISTS idx_package_dist_tags_package ON package_dist_tags(package_name, version);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (2);
