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
    name: "nv",
    title: "NV",
    latestVersion: "1.4.10",
  },
];
