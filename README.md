# NV

`nv` — пакетный менеджер и publish CLI.

Он делает четыре вещи:
- ставит пакеты из серверного реестра;
- показывает метаданные, версии и `dist-tags`;
- умеет `view`, как `npm view`, но под NV-пакеты;
- публикует новые версии прямо из терминала.

`nvd` — backend этого же репозитория. Он хранит индекс пакетов в SQLite, артефакты локально на диске и отдаёт catalog, details, resolve, `view`, bootstrap manifest и publish API.

## Установка

Linux:

```sh
curl -fsSL https://sosiskibot.ru/install/nv.sh | sh
```

Windows:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -Command "irm https://sosiskibot.ru/install/nv.ps1 | iex"
```

Повторный запуск install-скрипта обновляет установленный `nv`.

## Команды

```text
nv install | i <package[@version]> [--dir <path>]
nv uninstall | remove | rm <package>
nv update [package ...]
nv outdated [package ...] [--json]
nv list | ls
nv search | find [query]
nv info <package>
nv view | show | v <package[@version]> [field] [--json] [--os <linux|windows|all>]
nv pack [--manifest <file>] [--out <file>]
nv publish [--manifest <file>] [--tag <tag>] [--dry-run]
nv login --token <token> [--server <url>]
nv logout
nv whoami
nv server
nv version
nv help
```

## Базовый поток

Установить пакет:

```sh
nv i nv
```

Посмотреть, что лежит в реестре:

```sh
nv view nv homepage
nv view nv versions --json
nv view nv dist_tags
nv view nv@latest version
```

Проверить обновления:

```sh
nv outdated
nv update
```

## Что хранит сервер

`nvd` не зависит от GitHub как от source of truth.

Сервер хранит:
- индекс пакетов в SQLite;
- каждый опубликованный файл локально на диске;
- каждую опубликованную версию отдельно;
- `dist-tags` по пакетам;
- README и notes по версиям.

Минимальные зависимости сервера:
- `sqlite3`
- любой reverse proxy перед `nvd`

## Публикация из терминала

### 1. Подними backend

```sh
curl -fsSL https://github.com/Perdonus/NV/releases/download/v1.4.2/nvd-linux.tar.gz -o /tmp/nvd-linux.tar.gz
mkdir -p /opt/nvd/current
tar -xzf /tmp/nvd-linux.tar.gz -C /opt/nvd/current
cp /opt/nvd/current/install/nvd.service /etc/systemd/system/nvd.service
cat >/etc/nvd.env <<'EOF'
NVD_PUBLISH_TOKEN=<token>
NVD_PUBLIC_BASE_URL=https://sosiskibot.ru/nv/api
NVD_FILES_DIR=/var/www/neuralv/nv/files
EOF
systemctl daemon-reload
systemctl enable --now nvd.service
```

Что отдаёт `nvd`:
- `GET /packages`
- `GET /packages/details`
- `GET /packages/resolve`
- `GET /packages/view`
- `GET /bootstrap/manifest`
- `GET /files/*`
- `GET /whoami`
- `POST /publish`

Схема хранения: [docs/server-layout.md](/root/NV/docs/server-layout.md)

`nvd` читает seed-каталог из `registry/packages.json`, а готовые публичные файлы может отдавать напрямую из `NVD_FILES_DIR`. Это позволяет сразу перевести `/nv/api` на живой backend, не оставляя статический shim.

### 2. Один раз авторизуйся

```sh
nv login --server https://sosiskibot.ru/nv/api --token <token>
```

Проверить токен:

```sh
nv whoami
```

### 3. Подготовь `nv.package.json`

Пример лежит в [docs/publish-manifest.example.json](/root/NV/docs/publish-manifest.example.json).

Минимальный манифест:

```json
{
  "name": "project",
  "version": "1.0.0",
  "title": "Project",
  "description": "Короткое описание пакета.",
  "homepage": "https://example.org/project",
  "aliases": ["project-cli"],
  "dist_tags": ["latest"],
  "readme": "README.md",
  "variants": [
    {
      "id": "linux",
      "label": "Linux",
      "os": "linux",
      "default": true,
      "artifact": "dist/project-linux.tar.gz",
      "file_name": "project-linux.tar.gz",
      "install_strategy": "linux-portable-tar"
    },
    {
      "id": "windows",
      "label": "Windows",
      "os": "windows",
      "default": true,
      "artifact": "dist/project.exe",
      "file_name": "project.exe",
      "install_strategy": "windows-self-binary"
    }
  ]
}
```

Что важно:
- `name` — короткое имя пакета, без `@scope`;
- `version` — semver;
- каждый `variant` должен указывать реальный файл `artifact`;
- `dist_tags` определяют, что увидит `nv view` и что поставит `nv i <package>@<tag>`;
- если `dist_tags` не указаны, сервер трактует публикацию как `latest`.

### 4. Проверь пакет локально

```sh
nv pack
```

Это соберёт локальный `.nvpack.tgz` без отправки на сервер.

### 5. Опубликуй

```sh
nv publish
```

Или с явным manifest:

```sh
nv publish --manifest ./nv.package.json
```

Dry run:

```sh
nv publish --dry-run
```

Отдельный tag:

```sh
nv publish --tag beta
```

Что делает `publish`:
- читает `nv.package.json`;
- забирает артефакты из `artifact`;
- отправляет manifest и файлы на `nvd`;
- сервер сохраняет каждую версию локально;
- сервер сразу начинает отдавать `view`, `resolve` и прямые download URL.

То есть выкладывать проект можно полностью из терминала, без ручного редактирования реестра и без публикации файлов на GitHub.

## Структура репозитория

```text
cmd/nv      CLI
cmd/nvd     backend
install/    bootstrap scripts для Linux/Windows
registry/   seed registry
site/nv     статический NV сайт
var/nvd     серверное хранилище nvd
```

## Сайт

Источник NV-сайта лежит в `site/nv/`.

Он намеренно простой:
- сверху — install surface для `nv` с переключением ОС;
- ниже — все пакеты с командами вида `nv i package`;
- без header, footer и витрины.

Для деплоя достаточно синхронизировать `site/nv/` в `/var/www/neuralv/nv`, поднять `nvd` и проксировать `/nv/api` в backend.
