# Gift

Gift ist ein deklaratives UI-Toolkit fuer Go: SwiftUI-artige Views, typisierter
State und GPU-Rendering mit [Ebitengine](https://ebitengine.org/), ohne Browser
oder WebView. Im Fokus stehen Desktop- und Touch-Kiosk-Anwendungen, insbesondere
auf Raspberry Pi 4 und 5.

## Warum?

Go-Anwendungen sollen ihre Oberflaeche direkt in Go beschreiben koennen, ohne
einen Web-Stack mitzubringen. Gift verbindet kleine, kombinierbare Views mit
gezielten State-Updates statt staendiger kompletter Neuaufbauten. Virtuelle
Galerien, asynchrones Bildladen und begrenzte Ressourcenbudgets sind fuer
bildreiche Oberflaechen auf kleiner Hardware gedacht.

**In Entwicklung:** APIs koennen sich noch aendern. 1080p bei 60 Hz auf dem
Raspberry Pi ist ein Ziel, keine bereits bestaetigte Leistungsgarantie.

## So Sieht Es Aus

Echte Framebuffer-Aufnahmen der laufenden Beispiele, aufgenommen mit `giftauto`.

**Kitchen Sink:** Navigation, Tabs, Formulare, Bildschirmtastatur, Icons und
umschaltbares helles/dunkles Theme.

<img src="docs/screenshots/kitchensink.png" alt="Kitchen Sink im dunklen Theme mit Karten, Navigation, Bildern und Tab-Leiste" width="780">

| Bildergalerie | Effekte |
| --- | --- |
| ![Virtuelle Galerie mit generierten Beispielbildern im Masonry-Layout](docs/screenshots/gallery.png) | ![Experimentelles Glas-Panel vor farbigen Kacheln und Karten mit Schatten](docs/screenshots/effects.png) |
| Masonry/Justified, Auswahl, Datei-/HTTP-Quellen und Thumbnail-Cache. Die rote Kachel demonstriert einen Ladefehler. | Schatten, abgerundete Formen und experimentelles Glas mit einstellbarer Qualitaet. |

## Ausprobieren

Benoetigt **Go 1.27** und eine grafische Sitzung mit GPU-Unterstuetzung. Die
Beispiele bringen Inter als eingebettete Schrift mit. Fuer den Raspberry Pi
ist Raspberry Pi OS 64-Bit mit X11/XWayland und hardwarebeschleunigtem Mesa
vorgesehen; Builds sind ohne CGO moeglich.

Im geklonten Repository, jeweils ein Beispiel starten:

```sh
go run ./cmd/example-counter       # Einstieg: State, Buttons, Scrollen
go run ./cmd/example-kitchensink   # Uebersicht der UI-Komponenten
go run ./cmd/example-gallery      # Generiert eigene Beispielbilder
go run ./cmd/example-effects -quality=full
```

Eigene JPEG-/PNG-Bilder: `go run ./cmd/example-gallery -dir "$HOME/Pictures"`.
Bedienung per Maus, Tastatur oder Touch; die Galerie laesst sich auch ziehen.

## Eigene Anwendung

Mit `go get github.com/worldiety/gift` einbinden. Views sind Go-Funktionen;
State wird im Context angelegt und Aenderungen bauen die abhaengigen Views neu:

```go
func counter(ctx *gift.Context) gift.View {
    count := ctx.State("count", 0)
    n := ctx.Read(count)
    return ui.Window(ui.VStack(
        ui.Text(fmt.Sprintf("%d Klicks", n)),
        ui.Button(ui.Text("+1"), func() { count.Set(count.Get() + 1) }),
    ).Gap(16).Padding(24))
}
```

`gift.New(gift.Options{Root: counter})` erstellt die App, `backend.Run` aus
`github.com/worldiety/gift/backend/ebiten` oeffnet das Fenster. Schrift und
Fenstergroesse setzt die Anwendung explizit. Ein vollstaendiges Beispiel steht
in [`cmd/example-counter`](cmd/example-counter/main.go).

## Testen Und Automatisieren

```sh
go test ./...
go test -tags giftauto ./...
# Pixeltests mit echter GPU und vorhandenen Referenzbildern:
GIFT_REQUIRE_GOLDEN=1 go test -tags giftgpu ./...
```

Fuer Automation und Screenshots reicht das Build-Tag allein, ohne Zusatzdatei
oder Side-Effect-Import in der Anwendung:

```sh
go run -tags giftauto ./cmd/example-kitchensink
# In einem zweiten Terminal:
curl -fsS http://127.0.0.1:7391/tree
curl -fsS 'http://127.0.0.1:7391/screenshot?settle=8' -o screenshot.png
```

**Nicht mit `giftauto` ausliefern:** Es aktiviert eine unauthentifizierte lokale
Debug-Schnittstelle fuer Eingaben und Bildschirminhalte. Ohne Tag ist sie nicht
im Backend enthalten. Details: [`auto`](auto/doc.go),
[Screenshot-Reproduktion](docs/screenshots/README.md),
[Architektur und Ziele](PLAN.md).

## Lizenz

[BSD-2-Clause](LICENSE). Eingebettete Schriften und Icons haben eigene
Lizenzhinweise in ihren Paketverzeichnissen.
