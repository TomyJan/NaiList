import React from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import "./styles.css";
import { APP_VERSION } from "./version";
document.documentElement.dataset.version = APP_VERSION;
createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
