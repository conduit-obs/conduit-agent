import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./index.css";
import { App } from "./App";

const root = document.getElementById("root");
if (!root) {
  throw new Error(
    "conduit-quickstart: #root not found in index.html — refusing to mount.",
  );
}

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
