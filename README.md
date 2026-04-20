# NV

`nv` — пакетный менеджер и publish CLI.

`nvd` — backend этого же репозитория. Он хранит индекс пакетов в SQLite, артефакты локально на диске и отдаёт catalog, `view`, `resolve`, bootstrap manifest и publish API.

Главная идея:
- пакеты ставятся командами вида `nv i <package>`;
- пакет можно запросить по фиксированной версии: `nv i <package>@1.4.6`;
- пакет можно запросить по ветке обновлений: `nv i <package>@latest`, `nv i <package>@beta`, `nv i <package>@canary`;
- сами ветки обновлений задаёт автор пакета через `dist_tags`.

## Установка NV

Linux:

```sh
curl -fsSL https://sosiskibot.ru/install/nv.sh | sh
```

Windows:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -Command "irm https://sosiskibot.ru/install/nv.ps1 | iex"
```

Повторный запуск install-скрипта обновляет уже установленный `nv`.

## Команды

```text
nv install | i <package[@version|tag]> [--dir <path>]
nv uninstall | remove | rm <package>
nv update [package ...]
nv outdated [package ...] [--json]
nv list | ls
nv search | find [query]
nv info <package>
nv view | show | v <package[@version|tag]> [field] [--json] [--os <linux|windows|all>]
nv pack [--manifest <file>] [--out <file>]
nv publish [--manifest <file>] [--tag <tag>] [--tags <a,b,c>] [--dry-run] [--token <key>] [--server <url>]
nv login --token <key> [--server <url>]
nv logout
nv whoami
nv server
nv version
nv help
```

## Как работает спецификация пакета

NV понимает три формы:

```sh
nv i nv
nv i nv@1.4.6
nv i nv@beta
```

Что это значит:
- `nv i nv` — поставить то, что сейчас висит на `latest`;
- `nv i nv@1.4.6` — поставить конкретную semver-версию;
- `nv i nv@beta` — поставить версию, на которую сейчас указывает `beta`.

Теги не зашиты в клиент. Автор пакета может публиковать любые нормальные `dist_tags`:
- `latest`
- `beta`
- `stable`
- `canary`
- `nightly`
- и любые свои, если они состоят из букв, цифр, `.`, `_`, `-`.

Примеры:

```sh
nv i nv@latest
nv i nv@beta
nv view nv dist_tags
nv view nv@beta version
nv view nv@1.4.6 versions --json
```

## Что уже умеет backend

`nvd` уже поддерживает npm-подобную механику:
- хранение `dist_tags` по каждому пакету;
- выбор версии через `resolve`;
- `view` с `dist_tags`, списком версий и выбранной версией;
- сохранение старых версий в каталоге, чтобы можно было явно запросить `nv i <package>@1.4.4`;
- bootstrap manifest;
- локальное хранение артефактов на сервере, без зависимости от GitHub как source of truth.

## Ключ публикации

Для выкладки пакета нужен отдельный publish key.

Серверная сторона:
- ключ задаётся только на сервере через `NVD_PUBLISH_TOKEN`;
- сервер сверяет его только серверно;
- сервер не отдаёт этот ключ наружу никаким endpoint;
- клиент получает только `ok / unauthorized`, но не может вытащить ключ из API.

Клиентская сторона:
- разово: `nv login --token <key>`;
- или без сохранения: `nv publish --token <key>`;
- или через env: `NV_PUBLISH_TOKEN=<key>`.

Теги публикации:
- `--tag beta` — добавить один tag;
- `--tag canary --tag latest` — несколько повторяемых тегов;
- `--tags latest,beta,stable` — список тегов одной строкой.

Сейчас publish без ключа не должен использоваться. Это не публичная anonymous-операция.

## Полный поток выкладки пакета

Ниже один цельный сценарий. Этого файла достаточно, чтобы поднять publish и начать выкладывать пакеты.

### 1. Поднять backend `nvd`

Скачай готовый backend-архив из release:

```sh
curl -fsSL https://github.com/Perdonus/NV/releases/download/v1.4.6/nvd-linux.tar.gz -o /root/nvd-linux.tar.gz
mkdir -p /opt/nvd/current
tar -xzf /root/nvd-linux.tar.gz -C /opt/nvd/current
cp /opt/nvd/current/install/nvd.service /etc/systemd/system/nvd.service
```

Создай конфиг окружения:

```sh
cat >/etc/nvd.env <<'EOF_ENV'
NVD_ADDR=127.0.0.1:9640
NVD_DATA_DIR=/var/lib/nvd
NVD_FILES_DIR=/var/www/neuralv/nv/files
NVD_SEED_PATH=/opt/nvd/current/registry/packages.json
NVD_PUBLIC_BASE_URL=https://sosiskibot.ru/nv/api
NVD_PUBLISH_TOKEN=<сильный_секретный_ключ>
EOF_ENV
```

Запусти сервис:

```sh
systemctl daemon-reload
systemctl enable --now nvd.service
systemctl status nvd.service
```

Проверь backend локально:

```sh
curl --noproxy '*' http://127.0.0.1:9640/healthz
```

Ожидаемый ответ:

```text
ok
```

### 2. Проксировать API наружу

`nvd` должен жить за reverse proxy. Пример для nginx:

```nginx
location = /nv/api {
    return 301 /nv/api/;
}

location ^~ /nv/api/files/ {
    proxy_pass http://127.0.0.1:9640/files/;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
}

location ^~ /nv/api/ {
    proxy_pass http://127.0.0.1:9640/;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
}
```

После этого проверь внешние endpoints:

```sh
curl https://sosiskibot.ru/nv/api/packages?os=all
curl https://sosiskibot.ru/nv/api/bootstrap/manifest?platform=nv-linux
```

### 3. Подготовить пакет

В корне проекта создай `nv.package.json`.

Пример:

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
  "notes": "CHANGELOG.md",
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

Что здесь важно:
- `name` — короткое имя пакета без `@scope`;
- `version` — semver;
- `dist_tags` — ветки обновлений, на которые укажет эта публикация;
- `artifact` — реальный файл, который уйдёт на сервер;
- `default` — какой variant считать основным для своей ОС.

### 4. Подготовить ключ на клиенте

Есть три варианта.

Разово сохранить ключ:

```sh
nv login --server https://sosiskibot.ru/nv/api --token <key>
```

Или на один запуск:

```sh
nv publish --token <key>
```

Или через env:

```sh
export NV_PUBLISH_TOKEN=<key>
```

Проверка:

```sh
nv whoami
```

### 5. Проверить пакет локально

Без отправки на сервер:

```sh
nv pack
```

Это собирает локальный `.nvpack.tgz`, чтобы проверить manifest и набор файлов.

### 6. Выполнить dry-run publish

```sh
nv publish --dry-run
```

Это прогоняет тот же publish-контур, но без финальной записи версии в реестр.

### 7. Опубликовать

Просто публикация:

```sh
nv publish
```

Явный manifest:

```sh
nv publish --manifest ./nv.package.json
```

С отдельным тегом:

```sh
nv publish --tag beta
```

Можно публиковать сразу в несколько веток обновлений — просто перечисли их в `dist_tags` внутри manifest, а `--tag` используй как быстрый override/добавку.

### 8. Проверить результат

После публикации проверь:

```sh
nv view project
nv view project dist_tags
nv view project@beta version
nv i project@latest
nv i project@beta
nv i project@1.0.0
```

## Что реально делает `nv publish`

`nv publish`:
- читает `nv.package.json`;
- читает `README.md` и `notes`, если они указаны;
- забирает все variant-артефакты;
- отправляет manifest и файлы в `nvd`;
- `nvd` сохраняет артефакты локально на сервере;
- `nvd` обновляет индекс пакета, версии и `dist_tags`;
- после этого `view`, `resolve`, bootstrap и install начинают видеть новую версию.

То есть публикация идёт полностью из терминала. Без ручного редактирования реестра. Без ручного копирования файлов на GitHub release как обязательного шага.

## Как `dist_tags` ведут себя на практике

Если ты публикуешь:

- `1.0.0` с `dist_tags = ["latest"]`
- `1.1.0` с `dist_tags = ["beta"]`
- `1.0.1` с `dist_tags = ["latest", "stable"]`

то получится:
- `nv i project` -> `1.0.1`
- `nv i project@latest` -> `1.0.1`
- `nv i project@stable` -> `1.0.1`
- `nv i project@beta` -> `1.1.0`
- `nv i project@1.0.0` -> ровно `1.0.0`

Это тот же базовый принцип, что в `npm`: semver-версия и человекочитаемые каналы сосуществуют одновременно.

## Что хранит `nvd`

`nvd` хранит:
- `packages` — карточку пакета;
- `variants` — платформенные варианты;
- `releases` — конкретные опубликованные файлы;
- `package_dist_tags` — `latest`, `beta` и любые другие каналы;
- `package_versions` — README/notes по версии;
- сами бинарники в `NVD_FILES_DIR`.

Поэтому реестр и артефакты живут у тебя на сервере, а не обязаны качаться с GitHub.

## Текущий публичный контур NV

Сейчас live используются:
- сайт: `https://sosiskibot.ru/nv/`
- API: `https://sosiskibot.ru/nv/api`
- install Linux: `https://sosiskibot.ru/install/nv.sh`
- install Windows: `https://sosiskibot.ru/install/nv.ps1`

## Итог

Если коротко, для выкладки пакета нужен такой минимум:

1. поднять `nvd`;
2. задать `NVD_PUBLISH_TOKEN` на сервере;
3. подготовить `nv.package.json`;
4. авторизоваться через `nv login --token <key>` или `NV_PUBLISH_TOKEN`;
5. сделать `nv publish`.

Этого достаточно, чтобы пакет появился в реестре, начал резолвиться по `@latest/@beta/...` и ставился обычной командой `nv i <package>`.
