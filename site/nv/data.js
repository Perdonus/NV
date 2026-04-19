export const apiBase = "/nv/api";

export const installTargets = {
  linux: {
    label: "Linux",
    command: "curl -fsSL https://neuralvv.org/install/nv.sh | sh",
  },
  windows: {
    label: "Windows",
    command: 'powershell -NoProfile -ExecutionPolicy Bypass -Command "irm https://neuralvv.org/install/nv.ps1 | iex"',
  },
};

export const fallbackProjects = [
  {
    name: "@lvls/nv",
    title: "NV",
    latestVersion: "",
    commands: [],
  },
  {
    name: "@lvls/neuralv",
    title: "NeuralV",
    latestVersion: "",
    commands: [
      {
        label: "Установить",
        command: "nv install @lvls/neuralv",
      },
    ],
  },
];
