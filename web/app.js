const authMessage = document.getElementById("authMessage");
const authSection = document.getElementById("authSection");
const appSection = document.getElementById("appSection");
const vaultSelect = document.getElementById("vaultSelect");
const fileList = document.getElementById("fileList");

let token = localStorage.getItem("crypter_token") || "";

function setAuthMessage(text, isError = false) {
  authMessage.textContent = text;
  authMessage.className = `status ${isError ? "error" : "ok"}`;
}

async function request(path, options = {}) {
  const headers = options.headers || {};
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  const response = await fetch(path, { ...options, headers });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(data.error || `Erro em ${path}`);
  }
  return data;
}

async function authenticate(path) {
  const email = document.getElementById("email").value.trim();
  const password = document.getElementById("password").value;
  try {
    const data = await request(path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email, password }),
    });
    token = data.token;
    localStorage.setItem("crypter_token", token);
    setAuthMessage("Autenticado com sucesso.");
    authSection.style.display = "none";
    appSection.style.display = "block";
    await loadVaults();
  } catch (error) {
    setAuthMessage(error.message, true);
  }
}

async function loadVaults() {
  const vaults = await request("/api/vaults");
  vaultSelect.innerHTML = "";
  for (const vault of vaults) {
    const option = document.createElement("option");
    option.value = vault.id;
    option.textContent = `${vault.name} (#${vault.id})`;
    vaultSelect.appendChild(option);
  }
  if (vaults.length > 0) {
    await loadFiles();
  } else {
    fileList.innerHTML = "<li>Nenhum cofre criado.</li>";
  }
}

async function loadFiles() {
  const vaultId = vaultSelect.value;
  if (!vaultId) {
    fileList.innerHTML = "<li>Selecione um cofre.</li>";
    return;
  }
  const files = await request(`/api/vaults/${vaultId}/files`);
  fileList.innerHTML = "";
  if (files.length === 0) {
    fileList.innerHTML = "<li>Nenhum arquivo no cofre.</li>";
    return;
  }
  for (const file of files) {
    const li = document.createElement("li");
    const meta = document.createElement("span");
    meta.className = "file-meta";
    meta.textContent = `${file.originalFileName} (${file.sizeBytes} bytes)`;

    const actions = document.createElement("div");
    actions.className = "actions";

    const downloadButton = document.createElement("button");
    downloadButton.type = "button";
    downloadButton.textContent = "Baixar";
    downloadButton.className = "btn-primary";
    downloadButton.addEventListener("click", () => downloadFile(file.id, file.originalFileName));

    const deleteButton = document.createElement("button");
    deleteButton.type = "button";
    deleteButton.textContent = "Apagar";
    deleteButton.className = "btn-danger";
    deleteButton.addEventListener("click", () => deleteFile(file.id));

    actions.appendChild(downloadButton);
    actions.appendChild(deleteButton);
    li.appendChild(meta);
    li.appendChild(actions);
    fileList.appendChild(li);
  }
}

async function downloadFile(fileId, originalName) {
  const response = await fetch(`/api/files/${fileId}/download`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!response.ok) {
    const errorData = await response.json().catch(() => ({}));
    if (response.status === 401) {
      localStorage.removeItem("crypter_token");
      token = "";
      authSection.style.display = "block";
      appSection.style.display = "none";
      alert("Sua sessão expirou. Faça login novamente.");
      return;
    }
    alert(errorData.error || "Falha ao baixar arquivo.");
    return;
  }
  const blob = await response.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = originalName;
  a.click();
  URL.revokeObjectURL(url);
}

async function deleteFile(fileId) {
  if (!confirm("Deseja realmente apagar este arquivo?")) {
    return;
  }
  try {
    await request(`/api/files/${fileId}`, { method: "DELETE" });
    await loadFiles();
  } catch (error) {
    alert(error.message);
  }
}

document.getElementById("loginBtn").addEventListener("click", () => authenticate("/api/auth/login"));
document.getElementById("registerBtn").addEventListener("click", () => authenticate("/api/auth/register"));
document.getElementById("refreshVaultsBtn").addEventListener("click", loadVaults);
document.getElementById("refreshFilesBtn").addEventListener("click", loadFiles);
vaultSelect.addEventListener("change", loadFiles);

document.getElementById("vaultForm").addEventListener("submit", async (event) => {
  event.preventDefault();
  try {
    const name = document.getElementById("vaultName").value.trim();
    await request("/api/vaults", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name }),
    });
    document.getElementById("vaultName").value = "";
    await loadVaults();
  } catch (error) {
    alert(error.message);
  }
});

document.getElementById("uploadForm").addEventListener("submit", async (event) => {
  event.preventDefault();
  const vaultId = vaultSelect.value;
  const file = document.getElementById("fileInput").files[0];
  if (!vaultId || !file) {
    alert("Selecione cofre e arquivo.");
    return;
  }

  const formData = new FormData();
  formData.append("file", file);
  try {
    await request(`/api/vaults/${vaultId}/files`, {
      method: "POST",
      body: formData,
    });
    document.getElementById("fileInput").value = "";
    await loadFiles();
  } catch (error) {
    alert(error.message);
  }
});

if (token) {
  authSection.style.display = "none";
  appSection.style.display = "block";
  loadVaults().catch(() => {
    localStorage.removeItem("crypter_token");
    token = "";
    authSection.style.display = "block";
    appSection.style.display = "none";
  });
}
