import "@fontsource/ibm-plex-sans/400.css";
import "@fontsource/ibm-plex-sans/500.css";
import "@fontsource/ibm-plex-sans/600.css";
import "@fontsource/ibm-plex-mono/400.css";
import "@fontsource/ibm-plex-mono/500.css";
import "./app/styles.css";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./app/app";
import { applyTheme, readStoredTheme } from "./shared/lib/theme";

// Apply the stored theme before the first render to avoid a light flash.
const storedTheme = readStoredTheme();
const prefersDark = window.matchMedia?.("(prefers-color-scheme: dark)").matches ?? false;
applyTheme(storedTheme === "system" ? (prefersDark ? "dark" : "light") : storedTheme);

const root = document.getElementById("root");
if (!root) throw new Error("missing #root element");

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
