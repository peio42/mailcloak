const state = {
  status: null,
  domains: [],
  aliases: [],
  apps: [],
  authReady: false,
  systemReady: false,
};

const el = (id) => document.getElementById(id);

document.addEventListener("DOMContentLoaded", () => {
  el("adminToken").value = sessionStorage.getItem("mailcloakAdminToken") || "";
  el("tokenPanel").addEventListener("submit", (event) => {
    event.preventDefault();
    sessionStorage.setItem("mailcloakAdminToken", el("adminToken").value.trim());
    refreshAll();
  });
  bindTabs();
  bindSetupForm();
  bindResourceForms();
  el("refreshAll").addEventListener("click", refreshAll);
  refreshAll();
});

function bindTabs() {
  document.querySelectorAll(".tab").forEach((tab) => {
    tab.addEventListener("click", () => {
      if (tab.disabled) return;
      document.querySelectorAll(".tab").forEach((item) => item.classList.remove("is-active"));
      document.querySelectorAll(".view").forEach((item) => item.classList.remove("is-active"));
      tab.classList.add("is-active");
      el(`view-${tab.dataset.view}`).classList.add("is-active");
    });
  });
}

function bindSetupForm() {
  const form = el("setupForm");
  form.elements.provider.addEventListener("change", updateProviderFields);
  updateProviderFields();

  el("validateSetup").addEventListener("click", guard(async () => {
    await apiFetch("/api/setup/validate", {
      method: "POST",
      body: JSON.stringify({ config: readSetupConfig() }),
    });
    showToast("Configuration is valid");
  }));

  el("testIDP").addEventListener("click", guard(async () => {
    const result = await apiFetch("/api/idp/test", {
      method: "POST",
      body: JSON.stringify({ config: readSetupConfig() }),
    });
    showToast(`${result.provider} connection succeeded`);
  }));

  el("applySetup").addEventListener("click", guard(async () => {
    const body = {
      config: readSetupConfig(),
      init_db: form.elements.init_db.checked,
      test_idp: form.elements.test_idp.checked,
      overwrite: form.elements.overwrite.checked,
    };
    await apiFetch("/api/setup/apply", {
      method: "POST",
      body: JSON.stringify(body),
    });
    showToast("Setup applied");
    await refreshAll();
  }));
}

function bindResourceForms() {
  el("domainForm").addEventListener("submit", guard(async (event) => {
    event.preventDefault();
    const form = event.currentTarget;
    await apiFetch("/api/domains", {
      method: "POST",
      body: JSON.stringify({ domain_name: form.elements.domain_name.value }),
    });
    form.reset();
    await refreshDomains();
    showToast("Domain saved");
  }));

  el("aliasForm").addEventListener("submit", guard(async (event) => {
    event.preventDefault();
    const form = event.currentTarget;
    await apiFetch("/api/aliases", {
      method: "POST",
      body: JSON.stringify({
        alias_email: form.elements.alias_email.value,
        target_user: form.elements.target_user.value,
      }),
    });
    form.reset();
    await refreshAliases();
    showToast("Alias saved");
  }));

  el("aliasUserFilter").addEventListener("input", debounce(() => {
    renderAliases();
  }, 250));

  el("appForm").addEventListener("submit", guard(async (event) => {
    event.preventDefault();
    const form = event.currentTarget;
    const appID = form.elements.app_id.value.trim();
    if (state.apps.some((app) => app.app_id === appID)) {
      throw new Error("Application already exists; use the Password action to change its password");
    }
    await apiFetch("/api/apps", {
      method: "POST",
      body: JSON.stringify({
        app_id: appID,
        password: form.elements.password.value,
      }),
    });
    form.reset();
    await refreshApps();
    showToast("Application saved");
  }));

  el("appFilter").addEventListener("input", debounce(() => {
    renderApps();
  }, 250));
}

async function refreshAll() {
  try {
    await refreshStatus();
    if (state.systemReady) {
      await Promise.all([refreshDomains(), refreshAliases(), refreshApps()]);
    } else {
      clearResourceViews();
    }
  } catch (err) {
    if (err.status === 401) {
      setAuthReady(false);
      setSystemReady(false);
      clearResourceViews();
      activateView("overview");
      el("apiState").textContent = "Admin token required";
    }
    showToast(err.message, true);
  }
}

async function refreshStatus() {
  try {
    const [health, status] = await Promise.all([
      apiFetch("/api/health"),
      apiFetch("/api/setup/status"),
    ]);
    state.status = status;
    setAuthReady(true);
    setSystemReady(isSystemReady(status));
    el("apiState").textContent = health.status === "ok" ? "API online" : "API status unknown";
    el("configPathDisplay").value = status.config_path || "";
    el("setupForm").elements.sqlite_path.value = status.db_path || el("setupForm").elements.sqlite_path.value;
    renderStatus(status);
  } catch (err) {
    el("apiState").textContent = err.status === 401 ? "Admin token required" : "API unavailable";
    if (err.status === 401) {
      setAuthReady(false);
      setSystemReady(false);
      clearResourceViews();
    }
    throw err;
  }
}

async function refreshDomains() {
  state.domains = await apiFetch("/api/domains");
  renderDomains();
}

async function refreshAliases() {
  state.aliases = await apiFetch("/api/aliases");
  renderAliases();
}

async function refreshApps() {
  state.apps = await apiFetch("/api/apps");
  renderApps();
}

function renderStatus(status) {
  renderKV(el("configStatus"), [
    ["Path", status.config_path],
    ["Exists", badge(status.config_exists)],
    ["Valid", badge(status.config_valid)],
    ["Writable", badge(status.config_writable)],
    ["Error", status.config_error || status.config_writable_error || "-"],
  ]);

  renderKV(el("dbStatus"), [
    ["Path", status.db_path],
    ["Exists", badge(status.db_exists)],
    ["Initialized", badge(status.db_initialized)],
    ["Writable", badge(status.db_writable)],
    ["Error", status.db_error || status.db_writable_error || "-"],
  ]);
}

function isSystemReady(status) {
  return Boolean(
    status.config_exists &&
    status.config_valid &&
    status.config_writable &&
    status.db_exists &&
    status.db_initialized &&
    status.db_writable,
  );
}

function setAuthReady(ready) {
  state.authReady = ready;
  el("tokenPanel").hidden = ready;
  updateTabs();
}

function setSystemReady(ready) {
  state.systemReady = ready;
  updateTabs();
}

function updateTabs() {
  document.querySelectorAll(".tab").forEach((tab) => {
    const view = tab.dataset.view;
    const allowed =
      state.authReady &&
      (view === "overview" || view === "setup" || state.systemReady);
    tab.disabled = !allowed;
  });

  const active = document.querySelector(".tab.is-active");
  if (active && active.disabled) {
    activateView(state.authReady ? "overview" : "overview");
  }
}

function activateView(view) {
  document.querySelectorAll(".tab").forEach((item) => item.classList.toggle("is-active", item.dataset.view === view));
  document.querySelectorAll(".view").forEach((item) => item.classList.toggle("is-active", item.id === `view-${view}`));
}

function clearResourceViews() {
  state.domains = [];
  state.aliases = [];
  state.apps = [];
  el("domainsTable").replaceChildren(emptyRow(3, "Configuration and database must be ready"));
  el("aliasesTable").replaceChildren(emptyRow(4, "Configuration and database must be ready"));
  el("appsTable").replaceChildren(emptyRow(4, "Configuration and database must be ready"));
}

function renderKV(target, entries) {
  target.replaceChildren();
  entries.forEach(([key, value]) => {
    const dt = document.createElement("dt");
    dt.textContent = key;
    const dd = document.createElement("dd");
    if (value instanceof Node) {
      dd.append(value);
    } else {
      dd.textContent = value || "-";
    }
    target.append(dt, dd);
  });
}

function renderDomains() {
  const tbody = el("domainsTable");
  tbody.replaceChildren();
  if (state.domains.length === 0) {
    tbody.append(emptyRow(3, "No domains"));
    return;
  }
  state.domains.forEach((domain) => {
    const tr = document.createElement("tr");
    tr.append(
      textCell(domain.domain_name),
      nodeCell(statusIcon(domain.enabled)),
      actionsCell([
        actionButton(domain.enabled ? "Disable" : "Enable", () => patchDomain(domain.domain_name, !domain.enabled)),
        actionButton("Delete", () => deleteDomain(domain.domain_name), "danger"),
      ]),
    );
    tbody.append(tr);
  });
}

function renderAliases() {
  const tbody = el("aliasesTable");
  tbody.replaceChildren();
  const aliases = filteredAliases();
  if (aliases.length === 0) {
    tbody.append(emptyRow(4, "No aliases"));
    return;
  }
  aliases.forEach((alias) => {
    const tr = document.createElement("tr");
    tr.append(
      textCell(alias.alias_email),
      textCell(alias.target_user),
      nodeCell(statusIcon(alias.enabled)),
      actionsCell([
        actionButton(alias.enabled ? "Disable" : "Enable", () => patchAlias(alias.alias_email, !alias.enabled)),
        actionButton("Delete", () => deleteAlias(alias.alias_email), "danger"),
      ]),
    );
    tbody.append(tr);
  });
}

function renderApps() {
  const tbody = el("appsTable");
  tbody.replaceChildren();
  const apps = filteredApps();
  if (apps.length === 0) {
    tbody.append(emptyRow(4, "No applications"));
    return;
  }
  apps.forEach((app) => {
    const tr = document.createElement("tr");
    tr.append(
      textCell(app.app_id),
      nodeCell(senderList(app)),
      nodeCell(statusIcon(app.enabled)),
      actionsCell([
        actionButton("Password", () => showPasswordForm(app.app_id)),
        actionButton(app.enabled ? "Disable" : "Enable", () => patchApp(app.app_id, !app.enabled)),
        actionButton("Delete", () => deleteApp(app.app_id), "danger"),
      ], app.app_id),
    );
    tbody.append(tr);
  });
}

function filteredAliases() {
  const filter = el("aliasUserFilter").value.trim().toLowerCase();
  if (!filter) return state.aliases;
  return state.aliases.filter((alias) => (alias.target_user || "").toLowerCase().includes(filter));
}

function filteredApps() {
  const filter = el("appFilter").value.trim().toLowerCase();
  if (!filter) return state.apps;
  return state.apps.filter((app) => (app.app_id || "").toLowerCase().includes(filter));
}

function senderList(app) {
  const wrap = document.createElement("div");
  wrap.className = "sender-list";
  (app.senders || []).forEach((sender) => {
    const item = document.createElement("div");
    item.className = "sender-item";
    const addr = document.createElement("span");
    addr.textContent = sender.from_addr;
    const button = actionButton("Remove", () => deleteSender(app.app_id, sender.from_addr), "danger");
    item.append(addr, button);
    wrap.append(item);
  });

  const form = document.createElement("form");
  form.className = "sender-add";
  form.addEventListener("submit", guard(async (event) => {
    event.preventDefault();
    const input = form.elements.from_addr;
    await addSender(app.app_id, input.value);
    input.value = "";
  }));

  const input = document.createElement("input");
  input.name = "from_addr";
  input.type = "email";
  input.placeholder = (app.senders && app.senders.length > 0) ? "Add sender" : "sender@example.com";
  input.required = true;

  const button = document.createElement("button");
  button.type = "submit";
  button.className = "plus-button";
  button.title = `Allow sender for ${app.app_id}`;
  button.setAttribute("aria-label", `Allow sender for ${app.app_id}`);
  button.textContent = "+";

  form.append(input, button);
  wrap.append(form);
  return wrap;
}

async function patchDomain(domain, enabled) {
  await apiFetch(`/api/domains/${encodeURIComponent(domain)}`, {
    method: "PATCH",
    body: JSON.stringify({ enabled }),
  });
  await refreshDomains();
}

async function deleteDomain(domain) {
  if (!confirm(`Delete domain ${domain}?`)) return;
  await apiFetch(`/api/domains/${encodeURIComponent(domain)}`, { method: "DELETE" });
  await refreshDomains();
}

async function patchAlias(alias, enabled) {
  await apiFetch(`/api/aliases/${encodeURIComponent(alias)}`, {
    method: "PATCH",
    body: JSON.stringify({ enabled }),
  });
  await refreshAliases();
}

async function deleteAlias(alias) {
  if (!confirm(`Delete alias ${alias}?`)) return;
  await apiFetch(`/api/aliases/${encodeURIComponent(alias)}`, { method: "DELETE" });
  await refreshAliases();
}

async function patchApp(appID, enabled) {
  await apiFetch(`/api/apps/${encodeURIComponent(appID)}`, {
    method: "PATCH",
    body: JSON.stringify({ enabled }),
  });
  await refreshApps();
}

async function deleteApp(appID) {
  if (!confirm(`Delete application ${appID}?`)) return;
  await apiFetch(`/api/apps/${encodeURIComponent(appID)}`, { method: "DELETE" });
  await refreshApps();
}

async function rotateAppPassword(appID, password) {
  await apiFetch("/api/apps", {
    method: "POST",
    body: JSON.stringify({ app_id: appID, password }),
  });
  await refreshApps();
  showToast("Application password changed");
}

async function deleteSender(appID, fromAddr) {
  await apiFetch(`/api/apps/${encodeURIComponent(appID)}/senders/${encodeURIComponent(fromAddr)}`, {
    method: "DELETE",
  });
  await refreshApps();
}

async function addSender(appID, fromAddr) {
  await apiFetch(`/api/apps/${encodeURIComponent(appID)}/senders`, {
    method: "POST",
    body: JSON.stringify({ from_addr: fromAddr }),
  });
  await refreshApps();
  showToast("Sender allowed");
}

function readSetupConfig() {
  const form = el("setupForm");
  const provider = form.elements.provider.value;
  const ttl = Number(form.elements.cache_ttl_seconds.value || 120);
  const config = {
    idp: {
      provider,
      keycloak: {
        cache_ttl_seconds: ttl,
      },
      authentik: {
        cache_ttl_seconds: ttl,
      },
    },
    sqlite: {
      path: form.elements.sqlite_path.value,
    },
    policy: {
      idp_failure_mode: form.elements.idp_failure_mode.value,
    },
    sockets: {
      policy_socket: form.elements.policy_socket.value,
      socketmap_socket: form.elements.socketmap_socket.value,
      socket_owner_user: form.elements.socket_owner_user.value,
      socket_owner_group: form.elements.socket_owner_group.value,
      socket_mode: form.elements.socket_mode.value,
    },
    daemon: {
      user: form.elements.daemon_user.value,
    },
  };

  if (provider === "keycloak") {
    config.idp.keycloak.base_url = form.elements.base_url.value;
    config.idp.keycloak.realm = form.elements.realm.value;
    config.idp.keycloak.client_id = form.elements.client_id.value;
    config.idp.keycloak.client_secret = form.elements.client_secret.value;
  } else {
    config.idp.authentik.base_url = form.elements.base_url.value;
    config.idp.authentik.api_token = form.elements.api_token.value;
  }
  return config;
}

function updateProviderFields() {
  const provider = el("setupForm").elements.provider.value;
  document.querySelectorAll("[data-provider-fields]").forEach((group) => {
    group.hidden = group.dataset.providerFields !== provider;
  });
}

async function apiFetch(path, options = {}) {
  const headers = new Headers(options.headers || {});
  const token = (sessionStorage.getItem("mailcloakAdminToken") || "").trim();
  if (token) {
    headers.set("Authorization", `Bearer ${token}`);
  }
  if (options.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const response = await fetch(path, { ...options, headers });
  const text = await response.text();
  const data = text ? JSON.parse(text) : null;
  if (!response.ok) {
    const err = new Error((data && data.error) || `HTTP ${response.status}`);
    err.status = response.status;
    if (response.status === 401) {
      sessionStorage.removeItem("mailcloakAdminToken");
      el("adminToken").value = "";
      setAuthReady(false);
      setSystemReady(false);
    }
    throw err;
  }
  return data;
}

function guard(fn) {
  return async (event) => {
    try {
      await fn(event);
    } catch (err) {
      showToast(err.message, true);
    }
  };
}

function badge(value) {
  const span = document.createElement("span");
  span.className = value ? "badge ok" : "badge bad";
  span.textContent = value ? "Yes" : "No";
  return span;
}

function statusIcon(value) {
  const span = document.createElement("span");
  span.className = value ? "status-icon ok" : "status-icon bad";
  span.title = value ? "Enabled" : "Disabled";
  span.setAttribute("aria-label", value ? "Enabled" : "Disabled");
  span.textContent = value ? "✓" : "✕";
  return span;
}

function textCell(value) {
  const td = document.createElement("td");
  td.textContent = value || "-";
  return td;
}

function nodeCell(node) {
  const td = document.createElement("td");
  td.append(node);
  return td;
}

function actionsCell(buttons, appID = "") {
  const td = document.createElement("td");
  td.className = "right";
  const wrap = document.createElement("div");
  wrap.className = "row-actions";
  buttons.forEach((button) => wrap.append(button));
  td.append(wrap);
  if (appID) {
    const form = document.createElement("form");
    form.className = "password-form";
    form.hidden = true;
    form.dataset.appId = appID;
    form.addEventListener("submit", guard(async (event) => {
      event.preventDefault();
      const input = form.elements.password;
      await rotateAppPassword(appID, input.value);
      input.value = "";
      form.hidden = true;
    }));

    const input = document.createElement("input");
    input.name = "password";
    input.type = "password";
    input.placeholder = "new password";
    input.required = true;

    const submit = document.createElement("button");
    submit.type = "submit";
    submit.textContent = "Save";
    submit.className = "primary";

    form.append(input, submit);
    td.append(form);
  }
  return td;
}

function showPasswordForm(appID) {
  document.querySelectorAll(".password-form").forEach((form) => {
    form.hidden = form.dataset.appId !== appID || !form.hidden;
    if (!form.hidden) {
      const input = form.elements.password;
      input.focus();
    }
  });
}

function actionButton(label, handler, className = "") {
  const button = document.createElement("button");
  button.type = "button";
  button.textContent = label;
  if (className) button.className = className;
  button.addEventListener("click", async () => {
    try {
      button.disabled = true;
      await handler();
    } catch (err) {
      showToast(err.message, true);
    } finally {
      button.disabled = false;
    }
  });
  return button;
}

function emptyRow(colspan, message) {
  const tr = document.createElement("tr");
  const td = document.createElement("td");
  td.colSpan = colspan;
  td.textContent = message;
  td.className = "muted";
  tr.append(td);
  return tr;
}

function showToast(message, isError = false) {
  const toast = el("toast");
  toast.textContent = message;
  toast.className = isError ? "toast error" : "toast";
  toast.hidden = false;
  clearTimeout(showToast.timer);
  showToast.timer = setTimeout(() => {
    toast.hidden = true;
  }, 4200);
}

function debounce(fn, delay) {
  let timer = null;
  return (...args) => {
    clearTimeout(timer);
    timer = setTimeout(() => fn(...args), delay);
  };
}
