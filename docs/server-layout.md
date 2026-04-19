# Server Layout

`nvd` хранит всё локально. GitHub не является обязательным upstream для выдачи файлов.

Базовая структура:

```text
var/nvd/
  v1/
    nvd.sqlite
    files/
      @lvls-nv/
        nv-linux/
          1.4.0/
            nv-linux.tar.gz
        nv-windows/
          1.4.0/
            nv.exe
      @scope-project/
        linux/
          1.0.0/
            project-linux.tar.gz
        windows/
          1.0.0/
            project.exe
```

Что лежит в SQLite:
- `packages` — пакет и его базовые метаданные
- `package_aliases` — алиасы пакета
- `variants` — системные варианты пакета
- `releases` — файл для конкретной версии и варианта
- `package_versions` — README и notes для версии
- `package_dist_tags` — `latest`, `beta` и любые другие теги

Что умеет backend:
- `/packages` — каталог
- `/packages/details` — карточка пакета
- `/packages/resolve` — конкретный файл под install/update
- `/packages/view` — metadata/versions/tags, как у `npm view`
- `/bootstrap/manifest` — bootstrap для install-скриптов
- `/files/...` — прямая выдача локально сохранённых файлов
- `/publish` — загрузка новой версии прямо из CLI

Если проект публикуется через `nv publish`, сервер:
1. сохраняет артефакт в `files/`
2. обновляет release index
3. сохраняет `dist-tags`
4. сохраняет README/notes версии
5. начинает сразу отдавать новый `view/resolve/download`
