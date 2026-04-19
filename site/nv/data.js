export const apiBase = "/nv/api";

const siteOrigin = typeof window !== "undefined" && window.location?.origin
  ? window.location.origin
  : "https://sosiskibot.ru";

export const installTargets = {
  linux: {
    label: "Linux",
    command: `curl -fsSL ${siteOrigin}/install/nv.sh | sh`,
  },
  windows: {
    label: "Windows",
    command: `powershell -NoProfile -ExecutionPolicy Bypass -Command "irm ${siteOrigin}/install/nv.ps1 | iex"`,
  },
};

export const fallbackProjects = [
  {
    name: "@lvls/nv",
    title: "NV",
    latestVersion: "1.4.0",
    commands: [],
  },
  {
    name: "@lvls/neuralv",
    title: "NeuralV",
    latestVersion: "1.5.33",
    commands: [
      {
        label: "Установить",
        command: "nv install @lvls/neuralv",
      },
    ],
  },
];
