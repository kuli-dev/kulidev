import { isDark } from "./theme";

// Mermaid is large, so it loads with the first diagram. Diagrams use the app's tokens;
// strict security: no scripts or HTML labels from diagram text.
type Mermaid = typeof import("mermaid").default;
let loaded: Promise<Mermaid> | null = null;
let themed = "";

function init(m: Mermaid) {
  const dark = isDark();
  const key = dark ? "dark" : "light";
  if (themed === key) return;
  themed = key;
  m.initialize({
    startOnLoad: false, securityLevel: "strict", theme: "base", fontFamily: "Archivo Variable, Archivo, system-ui, sans-serif",
    themeVariables: dark
      ? { background: "#111315", primaryColor: "#202327", primaryTextColor: "#f5f5f4", primaryBorderColor: "#f97316",
          lineColor: "#a1a1aa", secondaryColor: "#292d32", tertiaryColor: "#181a1d", textColor: "#f5f5f4",
          noteBkgColor: "#292d32", noteTextColor: "#f5f5f4", edgeLabelBackground: "#181a1d", clusterBkg: "#181a1d", clusterBorder: "#34383e" }
      : { background: "#fafaf9", primaryColor: "#ffffff", primaryTextColor: "#1c1917", primaryBorderColor: "#ea580c",
          lineColor: "#57534e", secondaryColor: "#f0efed", tertiaryColor: "#fafaf9", textColor: "#1c1917",
          edgeLabelBackground: "#ffffff", clusterBkg: "#f0efed", clusterBorder: "#e2e0dc" },
  });
}

let seq = 0;

// renderMermaid returns the diagram's SVG, or throws Mermaid's parse error.
export async function renderMermaid(code: string): Promise<string> {
  loaded ??= import("mermaid").then((m) => m.default);
  const m = await loaded;
  init(m);
  const { svg } = await m.render(`kuli-mmd-${++seq}`, code);
  return svg;
}
