import React from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import "./styles.css";
import { SITE_VERSION } from "./runtime";
document.documentElement.dataset.version = SITE_VERSION;
createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
