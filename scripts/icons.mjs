// Membuat templates/components/icons.html dari ikon Phosphor gaya regular (MIT).
// Jalankan: npm run vendor:icons. Nama di kiri dipakai template: {{template "icon" "home"}}.
import { readFileSync, writeFileSync } from "node:fs";

const icons = {
  home: "house",
  ticket: "ticket",
  activity: "clipboard-text",
  monitor: "desktop",
  alert: "warning",
  grid: "squares-four",
  users: "users",
  sliders: "sliders-horizontal",
  document: "file-text",
  key: "key",
  logout: "sign-out",
  qr: "qr-code",
  menu: "list",
  plus: "plus",
  check: "check",
  database: "database",
  sync: "arrows-clockwise",
  upload: "upload-simple",
  chart: "chart-bar",
  bell: "bell",
  phone: "device-mobile",
  send: "paper-plane-tilt",
  clock: "clock",
  inbox: "tray",
  pause: "pause-circle",
  wrench: "wrench",
  building: "buildings",
  door: "door",
  layers: "stack",
  list: "list-bullets",
  sidebar: "sidebar-simple",
  "caret-right": "caret-right",
  "caret-left": "caret-left",
  palette: "palette",
  close: "x",
  keyboard: "keyboard",
  heartbeat: "heartbeat",
  lightning: "lightning",
  shield: "shield-check",
};

const dir = "node_modules/@phosphor-icons/core/assets/regular/";
const body = (name) =>
  readFileSync(dir + name + ".svg", "utf8")
    .replace(/^<svg[^>]*>/, "")
    .replace(/<\/svg>\s*$/, "");

let out =
  "{{/* icon: ikon Phosphor regular 256×256 (fill currentColor), dibuat scripts/icons.mjs. " +
  'Pemakaian: {{template "icon" "home"}} */}}\n' +
  '{{define "icon"}}<svg viewBox="0 0 256 256" fill="currentColor" aria-hidden="true">\n';
let first = true;
for (const [key, file] of Object.entries(icons)) {
  out += `{{- ${first ? "if" : "else if"} eq . "${key}"}}${body(file)}\n`;
  first = false;
}
out += `{{- else}}${body("circle")}\n{{- end}}</svg>{{end}}\n`;
writeFileSync("templates/components/icons.html", out);
console.log(Object.keys(icons).length + " ikon ditulis");
