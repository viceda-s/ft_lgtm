const EXAMPLE_CODE = `package main

import "fmt"

func main() {
	fmt.Println("Hello, world!")
}
`;

const runButton = document.getElementById("run-button");
const runSpinner = document.getElementById("run-spinner");
const runLabel = document.getElementById("run-label");
const outputEl = document.getElementById("output");
const ipfsLinkEl = document.getElementById("ipfs-link");

let editor;

function setLoading(isLoading) {
  runButton.disabled = isLoading;
  runSpinner.hidden = !isLoading;
  runLabel.hidden = isLoading;
}

function clearResult() {
  outputEl.textContent = "";
  outputEl.classList.remove("error");
  ipfsLinkEl.hidden = true;
  ipfsLinkEl.removeAttribute("href");
}

function showOutput(text, isError) {
  outputEl.textContent = text;
  outputEl.classList.toggle("error", isError);
}

function showIpfsLink(rawUrl) {
  if (!rawUrl) {
    return;
  }
  let parsed;
  try {
    parsed = new URL(rawUrl);
  } catch {
    return;
  }
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    return;
  }
  ipfsLinkEl.href = parsed.href;
  ipfsLinkEl.hidden = false;
}

function renderResult(result) {
  const compileError = typeof result.compile_error === "string" ? result.compile_error : "";
  const stdout = typeof result.stdout === "string" ? result.stdout : "";
  const stderr = typeof result.stderr === "string" ? result.stderr : "";

  if (compileError) {
    showOutput(compileError, true);
    return;
  }

  if (result.success === true) {
    showOutput(stdout, false);
    showIpfsLink(result.ipfs_link);
    return;
  }

  const timedOutSuffix = result.timed_out ? "\n\n(execution timed out)" : "";
  showOutput((stderr || "Execution failed.") + timedOutSuffix, true);
}

async function runCode() {
  setLoading(true);
  clearResult();

  try {
    const response = await fetch("/api/execute", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ code: editor.getValue() }),
    });

    if (!response.ok) {
      throw new Error(`HTTP ${response.status}`);
    }

    const result = await response.json();
    renderResult(result);
  } catch {
    showOutput("Unable to contact the backend. Please try again.", true);
  } finally {
    setLoading(false);
  }
}

runButton.addEventListener("click", runCode);

require.config({ paths: { vs: "/vendor/monaco/min/vs" } });
require(["vs/editor/editor.main"], () => {
  editor = monaco.editor.create(document.getElementById("editor"), {
    value: EXAMPLE_CODE,
    language: "go",
    theme: "vs-dark",
    automaticLayout: true,
    minimap: { enabled: false },
  });
});
