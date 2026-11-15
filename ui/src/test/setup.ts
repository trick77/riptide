import "@testing-library/jest-dom/vitest";
import { beforeEach } from "vitest";
import i18n from "../i18n";

// jsdom's browser speaks en-US: the suites read German unless they switch,
// and no stored choice leaks from one test into the next.
beforeEach(async () => {
  localStorage.clear();
  await i18n.changeLanguage("de");
});
