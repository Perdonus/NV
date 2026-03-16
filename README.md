# NV

`nv` — отдельный пакетный менеджер для поставки NeuralV.

Сейчас в нём один пакет:

- `neuralv`

Команды:

```sh
nv install neuralv@latest
nv install neuralv@1.3.1
nv uninstall neuralv
nv -v
```

Установка самого `nv`:

Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/Perdonus/NV/main/install/nv.sh | sh
```

Windows:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -Command "irm https://raw.githubusercontent.com/Perdonus/NV/main/install/nv.ps1 | iex"
```
