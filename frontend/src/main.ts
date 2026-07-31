import "./styles/tokens.css";
import "./app.css";

import { renderAccounts } from "./views/accounts";
import { renderDashboard } from "./views/dashboard";
import { renderDoctorView } from "./views/doctor";

type Route = "quota" | "accounts" | "doctor";

const routes: Record<Route, { label: string; render: (el: HTMLElement) => void }> = {
  quota: { label: "Quota", render: renderDashboard },
  accounts: { label: "Accounts", render: renderAccounts },
  doctor: { label: "Doctor", render: renderDoctorView },
};

const app = document.querySelector<HTMLElement>("#app");
if (!app) throw new Error("#app not found");

const nav = document.createElement("nav");
nav.className = "topbar";
const main = document.createElement("main");
main.className = "content";
app.replaceChildren(nav, main);

for (const [key, route] of Object.entries(routes)) {
  const tab = document.createElement("button");
  tab.type = "button";
  tab.className = "topbar__tab";
  tab.dataset.route = key;
  tab.textContent = route.label;
  tab.addEventListener("click", () => {
    location.hash = `#${key}`;
  });
  nav.append(tab);
}

function currentRoute(): Route {
  const hash = location.hash.replace("#", "") as Route;
  return hash in routes ? hash : "quota";
}

function show(): void {
  const active = currentRoute();
  for (const tab of nav.querySelectorAll<HTMLElement>(".topbar__tab")) {
    tab.classList.toggle("topbar__tab--active", tab.dataset.route === active);
  }
  routes[active].render(main);
}

window.addEventListener("hashchange", show);
show();
