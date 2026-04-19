# NV

`nv` — пакетный менеджер и publish CLI для namespaced-пакетов `NV`.

Он решает четыре задачи:
- ставит пакеты из серверного реестра;
- показывает метаданные и доступные версии;
- показывает подробный `view`, как у `npm view`, но под NV-пакеты;
- публикует новые версии пакетов прямо из терминала.

`nvd` — отдельный backend этого же репозитория. Он хранит метаданные в SQLite, сами артефакты на диске и отдаёт catalog, details, resolve, view, bootstrap manifest и publish API.

## Установка

Linux:

```sh
curl -fsSL https://neuralvv.org/install/nv.sh | sh
```

Windows:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -Command "irm https://neuralvv.org/install/nv.ps1 | iex"
```

Повторный запуск install-скрипта обновляет существующую установку `nv`.

## Что нужно для сервера

`nvd` не зависит от GitHub как от source of truth.

Сервер хранит:
- индекс пакетов в SQLite;
- каждый опубликованный файл локально на диске;
- каждую опубликованную версию отдельно;
- `dist-tags` по пакетам;
- README/notes по версиям.

Минимальные зависимости сервера:
- `sqlite3`
- любой reverse proxy перед `nvd`

## Команды

```text
nv install | i <package[@version]> [--dir <path>]
nv uninstall | rm <package>
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
nv install @lvls/neuralv
```

Посмотреть, что лежит в реестре:

```sh
nv view @lvls/neuralv
nv view @lvls/neuralv versions --json
nv view @lvls/nv homepage
nv view @scope/project dist_tags
nv view @scope/project@beta version
```

Проверить обновления:

```sh
nv outdated
nv update
```

## Публикация из терминала

### 1. Подними backend

```sh
go run ./cmd/nvd \
  --addr :8080 \
  --data-dir ./var/nvd \
  --seed ./registry/packages.json \
  --public-base-url https://neuralvv.org/nv/api \
  --publish-token <token>
```

Что хранит `nvd`:
- SQLite с индексом пакетов и релизов;
- локальные артефакты в `var/nvd/v1/files/...`;
- README и notes по версиям;
- `dist-tags`, включая `latest`, `beta` и другие;
- seed-реестр из `registry/packages.json`;
- HTTP API:
  - `GET /packages`
  - `GET /packages/details`
  - `GET /packages/resolve`
  - `GET /packages/view`
  - `GET /bootstrap/manifest`
  - `GET /files/*`
  - `GET /whoami`
  - `POST /publish`

Подробная схема хранения: [docs/server-layout.md](/root/NV/docs/server-layout.md)

### 2. Один раз авторизуйся

```sh
nv login --server https://neuralvv.org/nv/api --token <token>
```

Проверить, что токен живой:

```sh
nv whoami
```

### 3. Подготовь `nv.package.json`

Пример лежит в [docs/publish-manifest.example.json](/root/NV/docs/publish-manifest.example.json).

Минимальный манифест:

```json
{
  "name": "@scope/project",
  "version": "1.0.0",
  "title": "Project",
  "description": "Короткое описание пакета.",
  "homepage": "https://example.org/project",
  "aliases": ["project"],
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

Что важно в publish manifest:
- `name` должен быть namespaced: `@scope/project`
- `version` должен быть semver
- каждый `variant` должен указывать реальный файл `artifact`
- `dist_tags` определяют, что увидит `nv view` и что поставит `nv install <package>@<tag>`
- если `dist_tags` не указаны, сервер трактует публикацию как `latest`

### 4. Проверь publish локально

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

Отдельный dist-tag при публикации:

```sh
nv publish --tag beta
```

Что делает `publish`:
- читает `nv.package.json`
- забирает артефакты из `artifact`
- отправляет manifest и файлы на `nvd`
- сервер сохраняет каждую версию локально
- сервер сразу начинает отдавать `view`, `resolve` и прямые download URL

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
- первая секция — команда установки `nv` с переключением ОС;
- ниже — все проекты с готовыми install-командами;
- без header/footer и без маркетинговой витрины.

Для деплоя достаточно синхронизировать содержимое `site/nv/` в `/var/www/neuralvv/nv` и прокинуть backend `nvd` под `/nv/api`.
