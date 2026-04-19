import { apiBase, fallbackProjects, installTargets } from "./data.js";

const heroCommand = document.querySelector("[data-copy-command]");
const heroCommandText = document.querySelector("[data-command-text]");
const osButtons = Array.from(document.querySelectorAll("[data-os-toggle]"));
const projectList = document.querySelector("#project-list");

const platform = `${navigator.userAgentData?.platform || navigator.platform || ""}`.toLowerCase();
let activeOs = platform.includes("win") ? "windows" : "linux";

function setHeroCommand(os) {
  const target = installTargets[os];
  if (!target) return;

  activeOs = os;
  heroCommandText.textContent = target.command;
  heroCommand.dataset.copyCommand = target.command;

  osButtons.forEach((button) => {
    button.setAttribute("aria-pressed", String(button.dataset.osToggle === os));
  });
}

function packageInstallCommand(pkg) {
  return `nv i ${pkg.name}`;
}

function createPackageCard(project) {
  const item = document.createElement("li");
  const card = document.createElement("article");
  card.className = "package-card";

  const top = document.createElement("div");
  top.className = "package-top";

  const title = document.createElement("div");
  title.className = "package-title";
  title.textContent = project.title || project.name;
  top.append(title);

  if (project.latestVersion) {
    const version = document.createElement("span");
    version.className = "package-version";
    version.textContent = project.latestVersion;
    top.append(version);
  }

  const commandButton = document.createElement("button");
  commandButton.className = "package-command";
  commandButton.type = "button";
  commandButton.dataset.copyCommand = packageInstallCommand(project);

  const commandMain = document.createElement("span");
  commandMain.className = "command-main";

  const commandText = document.createElement("code");
  commandText.className = "command-text";
  commandText.textContent = commandButton.dataset.copyCommand;

  const copyBadge = document.createElement("span");
  copyBadge.className = "copy-badge";
  copyBadge.setAttribute("aria-hidden", "true");
  copyBadge.innerHTML = `
    <span class="copy-badge__sheet copy-badge__sheet--back"></span>
    <span class="copy-badge__sheet copy-badge__sheet--front"></span>
  `;

  commandMain.append(commandText, copyBadge);
  commandButton.append(commandMain);

  card.append(top, commandButton);
  item.append(card);
  return item;
}

function renderProjects(projects) {
  const ordered = [...projects].sort((left, right) => {
    if (left.name === "nv") return -1;
    if (right.name === "nv") return 1;
    return (left.title || left.name).localeCompare(right.title || right.name, "ru");
  });
  projectList.replaceChildren(...ordered.map(createPackageCard));
}

function normalizePackagesResponse(payload) {
  if (!payload || !Array.isArray(payload.packages)) {
    return fallbackProjects;
  }
  return payload.packages.map((pkg) => ({
    name: pkg.name,
    title: pkg.title || pkg.name,
    latestVersion: pkg.latest_version || "",
  }));
}

async function loadProjects() {
  try {
    const response = await fetch(`${apiBase}/packages?os=all`, { headers: { accept: "application/json" } });
    if (!response.ok) {
      throw new Error(`http ${response.status}`);
    }
    const payload = await response.json();
    renderProjects(normalizePackagesResponse(payload));
  } catch {
    renderProjects(fallbackProjects);
  }
}

document.addEventListener("click", async (event) => {
  const target = event.target.closest("[data-copy-command]");
  if (!target) return;

  const command = target.dataset.copyCommand;
  if (!command) return;

  try {
    await navigator.clipboard.writeText(command);
  } catch {
    // noop
  }

  target.classList.add("is-copied");
  window.clearTimeout(target._copyTimer);
  target._copyTimer = window.setTimeout(() => {
    target.classList.remove("is-copied");
  }, 900);
});

osButtons.forEach((button) => {
  button.addEventListener("click", () => {
    setHeroCommand(button.dataset.osToggle);
  });
});

setHeroCommand(activeOs);
loadProjects();
