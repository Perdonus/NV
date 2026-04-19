import { apiBase, fallbackProjects, installTargets } from "./data.js";

const heroCommand = document.querySelector("[data-copy-command]");
const heroOsChip = document.querySelector("[data-command-os]");
const heroCommandText = document.querySelector("[data-command-text]");
const osButtons = Array.from(document.querySelectorAll("[data-os-toggle]"));
const projectList = document.querySelector("#project-list");

const platform = `${navigator.userAgentData?.platform || navigator.platform || ""}`.toLowerCase();
let activeOs = platform.includes("win") ? "windows" : "linux";

function setHeroCommand(os) {
  const target = installTargets[os];
  if (!target) return;

  activeOs = os;
  heroOsChip.textContent = target.label;
  heroCommandText.textContent = target.command;
  heroCommand.dataset.copyCommand = target.command;

  osButtons.forEach((button) => {
    button.setAttribute("aria-pressed", String(button.dataset.osToggle === os));
  });
}

function packageInstallCommand(pkg) {
  const matchingVariant = Array.isArray(pkg.variants)
    ? pkg.variants.find((variant) => variant?.os === activeOs)
    : null;
  if (matchingVariant?.install_command) {
    return matchingVariant.install_command;
  }
  if (pkg.name === "@lvls/nv") {
    const target = installTargets[activeOs] || installTargets.linux;
    return target.command;
  }
  return `nv install ${pkg.name}`;
}

function createCommandButton(command) {
  const button = document.createElement("button");
  button.className = "project-command";
  button.type = "button";
  button.dataset.copyCommand = command.command;

  const chip = document.createElement("span");
  chip.className = "command-chip";
  chip.textContent = command.label;

  const text = document.createElement("code");
  text.className = "command-text";
  text.textContent = command.command;

  button.append(chip, text);
  return button;
}

function createProjectCard(project) {
  const item = document.createElement("li");
  const card = document.createElement("article");
  card.className = "project-card";

  const top = document.createElement("div");
  top.className = "project-top";

  const title = document.createElement("div");
  title.className = "project-title";
  title.textContent = project.title || project.name;

  top.append(title);
  if (project.latestVersion) {
    const version = document.createElement("span");
    version.className = "project-version";
    version.textContent = project.latestVersion;
    top.append(version);
  }

  const commands = document.createElement("div");
  commands.className = "project-commands";
  const projectCommands = project.commands?.length
    ? project.commands
    : [
        {
          label: project.name === "@lvls/nv" ? (installTargets[activeOs]?.label || "Linux") : "Установить",
          command: packageInstallCommand(project),
        },
      ];
  commands.append(...projectCommands.map(createCommandButton));

  card.append(top, commands);
  item.append(card);
  return item;
}

function renderProjects(projects) {
  projectList.replaceChildren(...projects.map(createProjectCard));
}

function normalizePackagesResponse(payload) {
  if (!payload || !Array.isArray(payload.packages)) {
    return fallbackProjects;
  }
  return payload.packages.map((pkg) => ({
    name: pkg.name,
    title: pkg.title || pkg.name,
    latestVersion: pkg.latest_version || "",
    variants: Array.isArray(pkg.variants) ? pkg.variants : [],
    commands: [],
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
    // noop: fallback state still changes below
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
    loadProjects();
  });
});

setHeroCommand(activeOs);
loadProjects();
