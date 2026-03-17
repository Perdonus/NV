# NV

`nv` — пакетный менеджер для namespaced-пакетов NeuralV.

Команды:

```text
nv install <package[@version]>
nv uninstall <package>
nv list
nv search [query]
nv info <package>
nv version
nv help
```

Версии пакетов:

- `latest`
- semver 2.0.0

`install` используется и для первой установки, и для обновления уже установленного пакета.
Доступные пакеты и метаданные берутся из серверного реестра.
Canonical refs:

- `@lvls/neuralv`
- `@lvls/nv`

Если версия не указана, `nv` автоматически использует `latest`.
Сам `nv` обновляется через `nv install @lvls/nv`.

Установка `nv`:

Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/Perdonus/NV/main/install/nv.sh | sh
```

Windows:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -Command "irm https://raw.githubusercontent.com/Perdonus/NV/main/install/nv.ps1 | iex"
```

Повторный запуск install-скрипта обновляет существующую установку `nv`.
