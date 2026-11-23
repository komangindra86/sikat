// Dimuat sinkron di <head> sebelum CSS supaya mode gelap tidak berkedip putih (rujukan Casemix layout.md).
// Pilihan disimpan per browser: sikat.mode (terang | gelap | sistem), sikat.tema (Samudra | Mawar, kosong = Daun),
// sikat.kepadatan (padat | biasa), sikat.sidebar (tutup | buka, bawaan buka).
(function () {
  try {
    var akar = document.documentElement;
    var mode = localStorage.getItem("sikat.mode") || "terang";
    var gelap = mode === "gelap" || (mode === "sistem" && matchMedia("(prefers-color-scheme: dark)").matches);
    if (gelap) akar.dataset.semantik = "Gelap";
    akar.style.colorScheme = gelap ? "dark" : "light";
    var tema = localStorage.getItem("sikat.tema");
    if (tema === "Samudra" || tema === "Mawar") akar.dataset.tema = tema;
    if (localStorage.getItem("sikat.kepadatan") === "padat") akar.dataset.kepadatan = "Padat";
    if (localStorage.getItem("sikat.sidebar") === "tutup") akar.dataset.sidebar = "tutup";
  } catch (e) {}
})();
