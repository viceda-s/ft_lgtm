const EXAMPLE_CODE = `package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Structs and methods: Go's lightweight take on objects.
type Planet struct {
	Name   string
	Moons  int
	Radius float64 // km
}

func (p Planet) String() string {
	return fmt.Sprintf("%-8s %3d moons  radius %6.0f km", p.Name, p.Moons, p.Radius)
}

// Errors are plain values, returned next to the result.
func find(planets []Planet, name string) (Planet, error) {
	for _, p := range planets {
		if strings.EqualFold(p.Name, name) {
			return p, nil
		}
	}
	return Planet{}, errors.New("no planet named " + name)
}

func main() {
	planets := []Planet{
		{"Mercury", 0, 2440}, {"Venus", 0, 6052}, {"Earth", 1, 6371},
		{"Mars", 2, 3390}, {"Jupiter", 95, 69911}, {"Saturn", 146, 58232},
	}

	// Sort a slice with a closure.
	sort.Slice(planets, func(i, j int) bool { return planets[i].Moons > planets[j].Moons })
	for _, p := range planets {
		fmt.Println(p)
	}

	// Goroutines and channels: one worker per planet, results gathered here.
	moons := make(chan int)
	for _, p := range planets {
		go func(p Planet) { moons <- p.Moons }(p)
	}
	total := 0
	for range planets {
		total += <-moons
	}
	fmt.Println("\\nmoons in total:", total)

	// Maps group values by key.
	byKind := map[string][]string{}
	for _, p := range planets {
		kind := "rocky"
		if p.Radius > 20000 {
			kind = "giant"
		}
		byKind[kind] = append(byKind[kind], p.Name)
	}
	fmt.Println("giants:", strings.Join(byKind["giant"], ", "))

	if _, err := find(planets, "Pluto"); err != nil {
		fmt.Println("error:", err)
	}
}
`;

const C_EXAMPLE = `#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// Structs group related data.
typedef struct {
    const char *name;
    int moons;
    double radius_km;
} Planet;

// qsort takes a pointer to a comparison function.
static int by_moons(const void *a, const void *b) {
    const Planet *pa = a, *pb = b;
    return pb->moons - pa->moons;
}

int main(void) {
    Planet planets[] = {
        {"Mercury", 0, 2440}, {"Venus", 0, 6052}, {"Earth", 1, 6371},
        {"Mars", 2, 3390}, {"Jupiter", 95, 69911}, {"Saturn", 146, 58232},
    };
    size_t count = sizeof planets / sizeof planets[0];

    qsort(planets, count, sizeof planets[0], by_moons);

    int total = 0;
    for (size_t i = 0; i < count; i++) {
        printf("%-8s %3d moons  radius %6.0f km\\n", planets[i].name, planets[i].moons, planets[i].radius_km);
        total += planets[i].moons;
    }
    printf("\\nmoons in total: %d\\n", total);

    // Manual memory management: build a string on the heap, then free it.
    char *giants = malloc(64);
    giants[0] = '\\0';
    for (size_t i = 0; i < count; i++) {
        if (planets[i].radius_km > 20000) {
            if (giants[0] != '\\0') strcat(giants, ", ");
            strcat(giants, planets[i].name);
        }
    }
    printf("giants: %s\\n", giants);
    free(giants);
    return 0;
}
`;

const RUST_EXAMPLE = `use std::collections::HashMap;
use std::fmt;

// Structs get behaviour from trait implementations.
struct Planet {
    name: &'static str,
    moons: u32,
    radius_km: f64,
}

impl fmt::Display for Planet {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "{:<8} {:>3} moons  radius {:>6.0} km", self.name, self.moons, self.radius_km)
    }
}

// Errors are values: Result<T, E> instead of exceptions.
fn find<'a>(planets: &'a [Planet], name: &str) -> Result<&'a Planet, String> {
    planets
        .iter()
        .find(|p| p.name.eq_ignore_ascii_case(name))
        .ok_or(format!("no planet named {name}"))
}

fn main() {
    let mut planets = vec![
        Planet { name: "Mercury", moons: 0, radius_km: 2440.0 },
        Planet { name: "Venus", moons: 0, radius_km: 6052.0 },
        Planet { name: "Earth", moons: 1, radius_km: 6371.0 },
        Planet { name: "Mars", moons: 2, radius_km: 3390.0 },
        Planet { name: "Jupiter", moons: 95, radius_km: 69911.0 },
        Planet { name: "Saturn", moons: 146, radius_km: 58232.0 },
    ];

    // Sorting with a closure.
    planets.sort_by(|a, b| b.moons.cmp(&a.moons));
    for p in &planets {
        println!("{p}");
    }

    // Iterators chain without loops.
    let total: u32 = planets.iter().map(|p| p.moons).sum();
    println!("\\nmoons in total: {total}");

    // HashMaps and pattern matching.
    let mut by_kind: HashMap<&str, Vec<&str>> = HashMap::new();
    for p in &planets {
        let kind = match p.radius_km {
            r if r > 20000.0 => "giant",
            _ => "rocky",
        };
        by_kind.entry(kind).or_default().push(p.name);
    }
    println!("giants: {}", by_kind["giant"].join(", "));

    match find(&planets, "Pluto") {
        Ok(p) => println!("found {}", p.name),
        Err(e) => println!("error: {e}"),
    }
}
`;

const ASSEMBLYSCRIPT_EXAMPLE = `// AssemblyScript is TypeScript with WebAssembly types such as i32 and f64.
class Planet {
  constructor(public name: string, public moons: i32, public radiusKm: i32) {}

  toString(): string {
    return this.name.padEnd(8) + " " + this.moons.toString().padStart(3)
      + " moons  radius " + this.radiusKm.toString().padStart(6) + " km";
  }
}

const planets: Planet[] = [
  new Planet("Mercury", 0, 2440), new Planet("Venus", 0, 6052),
  new Planet("Earth", 1, 6371), new Planet("Mars", 2, 3390),
  new Planet("Jupiter", 95, 69911), new Planet("Saturn", 146, 58232),
];

// Arrays take typed callbacks.
planets.sort((a: Planet, b: Planet): i32 => b.moons - a.moons);
for (let i = 0; i < planets.length; i++) {
  console.log(planets[i].toString());
}

// i32 arithmetic compiles to plain WebAssembly instructions.
let total: i32 = 0;
for (let i = 0; i < planets.length; i++) {
  total += planets[i].moons;
}
console.log("\\nmoons in total: " + total.toString());

// filter, map and join from the standard library.
const giants = planets
  .filter((p: Planet): bool => p.radiusKm > 20000)
  .map<string>((p: Planet): string => p.name);
console.log("giants: " + giants.join(", "));
`;

const EXAMPLES = { go: EXAMPLE_CODE, c: C_EXAMPLE, rust: RUST_EXAMPLE, assemblyscript: ASSEMBLYSCRIPT_EXAMPLE };

const MONACO_LANGUAGES = { go: "go", c: "c", rust: "rust", assemblyscript: "typescript" };

const runButton = document.getElementById("run-button");
const languageSelect = document.getElementById("language");
const runSpinner = document.getElementById("run-spinner");
const runLabel = document.getElementById("run-label");
const outputEl = document.getElementById("output");
const ipfsLinkEl = document.getElementById("ipfs-link");
const runMetaEl = document.getElementById("run-meta");
const runTimingsEl = document.getElementById("run-timings");
const traceLinkEl = document.getElementById("trace-link");
const shareButton = document.getElementById("share-button");
const shareBoxEl = document.getElementById("share-box");
const shareUrlEl = document.getElementById("share-url");
const shareHintEl = document.getElementById("share-hint");

const SHARE_ID_PATTERN = /^[a-z2-7]{8}(?:[a-z2-7]{8})?$/;

let editor;
const models = {};
let shareId = null;

function setLoading(isLoading) {
  runButton.disabled = isLoading;
  runSpinner.hidden = !isLoading;
  runLabel.hidden = isLoading;
}

function clearResult() {
  outputEl.textContent = "Compiling and running…";
  outputEl.classList.remove("error");
  ipfsLinkEl.hidden = true;
  ipfsLinkEl.removeAttribute("href");
  runMetaEl.hidden = true;
  traceLinkEl.hidden = true;
  shareId = null;
  shareButton.hidden = true;
  shareBoxEl.hidden = true;
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

function grafanaTraceUrl(traceId) {
  const panes = {
    trace: {
      datasource: "tempo",
      queries: [{ refId: "A", datasource: { type: "tempo", uid: "tempo" }, queryType: "traceql", query: traceId }],
      range: { from: "now-1h", to: "now" },
    },
  };
  return `${location.protocol}//grafana.${location.host}/explore?schemaVersion=1&panes=${encodeURIComponent(JSON.stringify(panes))}`;
}

function showRunMeta(timings, traceId) {
  const stages = timings && typeof timings === "object" ? timings : {};
  runTimingsEl.textContent = [["compile", stages.compile], ["run", stages.execute], ["IPFS upload", stages.ipfs_upload]]
    .filter(([, ms]) => Number.isFinite(ms))
    .map(([label, ms]) => `${label} ${ms} ms`)
    .join(" · ");
  if (typeof traceId === "string" && /^[0-9a-f]{32}$/.test(traceId)) {
    traceLinkEl.href = grafanaTraceUrl(traceId);
    traceLinkEl.hidden = false;
  }
  runMetaEl.hidden = false;
}

function renderResult(result) {
  const compileError = typeof result.compile_error === "string" ? result.compile_error : "";
  const stdout = typeof result.stdout === "string" ? result.stdout : "";
  const stderr = typeof result.stderr === "string" ? result.stderr : "";
  const output = stdout + stderr;

  if (compileError) {
    showOutput(compileError, true);
  } else if (result.success === true) {
    showOutput(output || "(no output)", false);
  } else {
    let notes = "";
    if (result.timed_out) notes += "\n\n(execution timed out)";
    if (result.output_truncated) notes += "\n\n(output limit reached: the program was stopped and its output truncated)";
    showOutput((output || "Execution failed.") + notes, true);
  }
  showIpfsLink(result.ipfs_link);
  showRunMeta(result.timings_ms, result.trace_id);
  if (typeof result.share_id === "string" && SHARE_ID_PATTERN.test(result.share_id)) {
    shareId = result.share_id;
    shareButton.hidden = false;
  }
}

async function shareCode() {
  shareUrlEl.value = `${location.origin}/s/${shareId}`;
  shareBoxEl.hidden = false;
  shareUrlEl.focus();
  shareUrlEl.select();
  try {
    await navigator.clipboard.writeText(shareUrlEl.value);
    shareHintEl.textContent = "Link copied ✓";
  } catch {
    shareHintEl.textContent = "Press Ctrl+C (⌘C) to copy";
  }
}

async function loadSharedCode() {
  if (!location.pathname.startsWith("/s/")) {
    return null;
  }
  const id = location.pathname.slice("/s/".length);
  if (!SHARE_ID_PATTERN.test(id)) {
    showOutput("This share link is not valid.", true);
    return null;
  }
  try {
    const response = await fetch(`/api/share/${id}`);
    if (!response.ok) {
      showOutput(response.status === 404 ? "This share link does not exist." : `Could not load the shared code (HTTP ${response.status}).`, true);
      return null;
    }
    const shared = await response.json();
    if (typeof shared.code !== "string" || !Object.hasOwn(EXAMPLES, shared.language)) {
      return null;
    }
    showOutput("Shared code loaded. Press Run to execute it.", false);
    showIpfsLink(shared.ipfs_link);
    return { code: shared.code, language: shared.language };
  } catch {
    showOutput("Unable to contact the backend to load the shared code.", true);
    return null;
  }
}

async function runCode() {
  setLoading(true);
  clearResult();

  try {
    const response = await fetch("/api/execute", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ code: editor.getValue(), language: languageSelect.value }),
    });

    if (!response.ok) {
      const reason = (await response.text()).trim();
      showOutput(`Request failed (HTTP ${response.status})${reason ? `: ${reason}` : ""}`, true);
      return;
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
shareButton.addEventListener("click", shareCode);

const sharedCode = loadSharedCode();

require.config({ paths: { vs: "/vendor/monaco/min/vs" } });
require(["vs/editor/editor.main"], () => {
  (monaco.typescript ?? monaco.languages.typescript)?.typescriptDefaults.setDiagnosticsOptions({ noSemanticValidation: true });
  sharedCode.then((shared) => {
    for (const [language, example] of Object.entries(EXAMPLES)) {
      models[language] = monaco.editor.createModel(example, MONACO_LANGUAGES[language]);
    }
    if (shared) {
      languageSelect.value = shared.language;
      models[shared.language].setValue(shared.code);
    }
    editor = monaco.editor.create(document.getElementById("editor"), {
      model: models[languageSelect.value],
      theme: "vs-dark",
      automaticLayout: true,
      minimap: { enabled: false },
    });
  });
});

languageSelect.addEventListener("change", () => {
  editor?.setModel(models[languageSelect.value]);
});
