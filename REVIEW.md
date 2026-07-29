# Review: `refactor/hexagonal-phase1-domain`

Zakres: pełny diff brancha względem `main` (`0f592f2d..76d1d327`).

## Findings

### [P1] `make test`/`make build` nie działają na czystym środowisku Go 1.25

**Miejsce:** `Makefile:12-16`, `Makefile:33-43`

`GOENV` wymusza repozytoryjne, początkowo puste `GOMODCACHE` oraz `GOSUMDB=off`. Go 1.25 jest zadeklarowane w `go.mod` i przy standardowym mechanizmie `GOTOOLCHAIN=auto` samo jest modułem wymagającym weryfikacji. W efekcie podstawowy target kończy się przed kompilacją:

```text
go: golang.org/toolchain@v0.0.1-go1.25.0.linux-amd64: verifying module: checksum database disabled by GOSUMDB=off
make: *** [Makefile:43: test] Error 1
```

Próba z `GOTOOLCHAIN=local` także nie jest obejściem, jeśli lokalny bootstrap Go jest starszy (tutaj 1.24.5), bo `go.mod` wymaga 1.25. To łamie deklarowane „Makefile = source of truth”, onboarding i każde środowisko bez wcześniej zapełnionego cache. Nie należy globalnie wyłączać sumdb; jeśli wymagany jest tryb offline, powinien być osobnym, świadomie wybranym targetem korzystającym z przygotowanego cache/toolchainu.

### [P1] Nowa warstwa service nie jest podłączona do produkcyjnego przepływu runów

**Miejsce:** `internal/service/service.go:79-141`, `internal/web/handler_run.go:30-167`, `internal/web/server.go:60-101`

`RunService` istnieje wyłącznie w testach. `Server` przechowuje tylko `ProjectService`; nie tworzy ani nie wywołuje `RunService`. `handleRunProject` nadal sam wykonuje całą logikę use-case: buduje changed-set, tworzy rekord, inicjalizuje statusy, uruchamia goroutine, wybiera runner, aktualizuje postęp i finalizuje run. Analogicznie `WebhookService` nie ma żadnego produkcyjnego użycia.

To nie jest zakończona migracja do architektury heksagonalnej: istnieją dwie implementacje orkiestracji, z których testowana service-layer jest martwa, a faktyczne zachowanie nadal siedzi w adapterze HTTP. Zmiana w jednej ścieżce nie trafi do drugiej. Przykładowo `RunService.Start` nie przekazuje `WorkDir`, variables, output writerów ani callbacków postępu, więc nie może zastąpić obecnego handlera bez dalszej pracy. Należy zbudować adapter implementujący port runnera, wstrzyknąć `RunService` w composition root i sprowadzić handler do dekodowania/wywołania use-case/enkodowania odpowiedzi; potem usunąć duplikat.

### [P1] `PipelineRunner` nie ma żadnego produkcyjnego adaptera

**Miejsce:** `internal/domain/ports.go:39-55`, `internal/runner/runner.go:40-175`, `internal/domain/portscheck/portscheck_test.go:13-23`

Komentarz portu mówi, że implementują go `LocalExecutor`, `DockerExecutor` i `GitLabRunnerExecutor`, ale te typy mają wyłącznie `RunTask(ctx, task, runner.Options)`. Port wymaga `Run(ctx, project, changed, domain.RunnerOptions)`. Sygnatury i odpowiedzialności są różne, więc żaden z tych typów nie spełnia interfejsu. Testy compile-time sprawdzają tylko store'y, mimo że spec i odhaczone zadanie 4.2 wymagają asercji także dla każdego executora.

W praktyce `RunService` da się skonstruować tylko z mockiem testowym; produkcyjnego wiring nie można skompilować. Potrzebny jest adapter na poziomie całego pipeline'u (najpewniej osobny typ wybierający local/docker/gitlab-runner), który tłumaczy `domain.RunnerOptions` na opcje runnera, oraz compile-time assertion dla tego adaptera.

### [P1] Deklarowane bramki race/coverage nie są wykonywane w CI, a progi zostały po cichu obniżone

**Miejsce:** `.gitlab-ci.yml:34-49`, `scripts/test.sh:6-11`, `scripts/test_coverage.sh:86-113`

CI nadal uruchamia bezpośrednio `go test ./...` bez `-race` i bez `scripts/test_coverage.sh`. Samo wygenerowanie profilu oraz regex GitLaba tylko raportują coverage; nie egzekwują żadnego minimum. Dodatkowo skrypt, który miał być bramką, ustawia `domain` na **0%** i `runner` na **75%**, podczas gdy proposal/tasks deklarują odpowiednio 90% i 80%. Brak danych dla pakietu jest traktowany jako sukces (`skipping`), więc błędna nazwa/usunięty pakiet również omija gate.

To daje fałszywe poczucie ochrony dokładnie przy dużym refaktorze współbieżnego kodu. Job CI powinien wywoływać jeden kanoniczny target/skrypt z `-race`, a progi muszą odpowiadać zaakceptowanej specyfikacji i failować także przy braku oczekiwanych danych. Artefakt opisany jako Cobertura jest ponadto surowym profilem Go, nie XML-em Cobertura, więc raport GitLaba może go nie przyjąć.

### [P2] `internal/domain` łamie własną regułę zależności „zero internal dependencies”

**Miejsce:** `internal/domain/ports.go:3-9`, `internal/domain/ports.go:26,42,95`, `internal/domain/types.go:1-4`

Pakiet deklaruje, że zależy wyłącznie od standard library, lecz `ports.go` importuje `internal/model`, a publiczne porty i `Run` eksponują `model.Project`. To przeczy targetowi z designu („domain imports nothing”), requirementowi „Domain package has zero external dependencies” oraz odhaczonemu punktowi 6.3. Powstały model zależności ma zatem dwie wewnętrzne warstwy domenowe (`domain` i `model`) zamiast jednoznacznego rdzenia.

Trzeba jawnie wybrać jedną wersję architektury: przenieść typy pipeline'u do rdzenia domenowego (a `model` ewentualnie zostawić jako alias/adapter) albo zmienić dokumentację i test architektoniczny tak, aby `model` był oficjalnie dozwoloną zależnością. Obecny `portscheck` sprawdza kompatybilność store'ów, ale nie pilnuje reguły importów, więc ta regresja pozostaje niewidoczna.

### [P2] `ProjectService.Update` omija walidację i pozostawia merge semantyki w HTTP

**Miejsce:** `internal/service/service.go:41-51`, `internal/web/handler_project.go:127-161`

Metoda jest opisana jako „validates and updates”, ale sprawdza jedynie istnienie rekordu. Nie stosuje walidacji wymaganej przy `Create` i nie zachowuje pól istniejącego configu. Merge pól (`RepoPath`, `PipelineDir`, `PipelineFile`, `CreatedAt`) nadal wykonuje handler, przy czym `RepoURL`, `DefaultBranch`, `WebhookSecret`, `ExtraRepos` i pozostałe pola mogą zostać wyzerowane przez częściowy request. Błąd `Upsert` jest też zawsze mapowany przez handler na 500, podczas gdy błąd walidacji powinien być 400.

Jeśli PUT ma być pełnym zastąpieniem, handler nie powinien robić częściowego merge'u. Jeśli ma działać jak patch (tak zachowuje się obecny kod), cała reguła merge + walidacja powinna znaleźć się w service i mieć testy dla zachowania wszystkich pól.

## Weryfikacja

- `make test` — **FAIL** przed kompilacją z powodu `GOSUMDB=off` i pobierania toolchainu.
- `GOTOOLCHAIN=local make test` — **FAIL**, lokalny bootstrap to Go 1.24.5, a moduł wymaga Go 1.25.
- Pełnego wyniku testów nie da się wiarygodnie podać przez kanoniczny workflow brancha, dopóki pierwszy finding nie zostanie naprawiony.

## Ocena

**Request changes.** Sam podział dużych plików i ujednolicenie `TaskResult` są wartościowe, ale branch nie realizuje deklarowanej granicy architektonicznej: nowy use-case i port runnera są nieużywalne produkcyjnie, a bramki jakości są w praktyce wyłączone. Przed mergem co najmniej findings P1 powinny zostać zamknięte.
