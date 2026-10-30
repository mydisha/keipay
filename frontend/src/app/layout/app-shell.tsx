import { useCallback, useState } from "react";
import { Outlet } from "react-router";
import { useMode } from "@/shared/lib/mode";
import { strings } from "@/shared/lib/strings";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "@/shared/ui/sheet";
import { Brand } from "@/shared/ui/brand";
import { CommandPalette, useCommandPaletteShortcut } from "./command-palette";
import { NavLinks } from "./nav-links";
import { Sidebar } from "./sidebar";
import { TestModeBanner } from "./test-mode-banner";
import { TopBar } from "./top-bar";

export function AppShell() {
  const { mode } = useMode();
  const [menuOpen, setMenuOpen] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);

  const togglePalette = useCallback(() => setPaletteOpen((open) => !open), []);
  useCommandPaletteShortcut(togglePalette);

  return (
    <div className="flex min-h-dvh">
      <a
        href="#main"
        className="sr-only z-50 rounded-md bg-card px-3 py-2 focus:not-sr-only focus:fixed focus:top-2 focus:left-2"
      >
        {strings.shell.skipToContent}
      </a>
      <Sidebar />
      <Sheet open={menuOpen} onOpenChange={setMenuOpen}>
        <SheetContent side="left" closeLabel={strings.nav.closeMenu} className="bg-sidebar p-2 pt-3">
          <SheetTitle asChild>
            <div className="flex h-9 items-center px-2.5">
              <Brand />
            </div>
          </SheetTitle>
          <SheetDescription className="sr-only">{strings.nav.label}</SheetDescription>
          <NavLinks onNavigate={() => setMenuOpen(false)} />
        </SheetContent>
      </Sheet>
      <div className="flex min-w-0 flex-1 flex-col">
        <TopBar onOpenMenu={() => setMenuOpen(true)} onOpenPalette={() => setPaletteOpen(true)} />
        {mode === "test" && <TestModeBanner />}
        <main
          id="main"
          tabIndex={-1}
          className="mx-auto grid w-full max-w-7xl min-w-0 flex-1 content-start gap-8 px-4 py-8 outline-none md:px-6 md:py-10 lg:px-8 [&>*]:min-w-0"
        >
          <Outlet />
        </main>
      </div>
      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />
    </div>
  );
}
