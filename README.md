# NV

`nv` — пакетный менеджер для пакетов NeuralV.

Доступные пакеты:

- `neuralv`
- `nv`

Команды:

```text
nv install <package[@version]>
nv uninstall <package>
nv version
nv help
```

Версии пакетов:

- `latest`
- строгий semver `1.2.3`

`install` используется и для первой установки, и для обновления уже установленного пакета. Сам `nv` обновляется через `nv install nv@latest`.

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
