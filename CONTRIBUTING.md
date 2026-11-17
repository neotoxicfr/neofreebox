# Contribuer

Merci de ton intérêt ! Une issue, une idée ou une pull request sont les bienvenues. En
participant, tu acceptes le [code de conduite](CODE_OF_CONDUCT.md).

## Prérequis

- **Go 1.27+** (la version de `go.mod` fait foi ; la CI l'installe à partir de là)
- **golangci-lint v2.15+**, compilé avec une version de Go ≥ celle de `go.mod`
- **Une chaîne C** (gcc/clang), uniquement pour `go test -race`
- **lefthook** *(facultatif)* : installe les hooks git de `lefthook.yml`
- **actionlint** *(facultatif)* : valide les modifications de workflows

```bash
neofreebox pair -o freebox.token   # une fois, sur le réseau de la box
make dev    # sert sur :8080, données dans ./data
```

## Conventions du projet

- **Pas d'étape de build pour le front** : `html/template` et JS/CSS natifs, embarqués
  dans le binaire. Pas de npm.
- **Ordinateur et mobile comptent autant** : chaque modification d'interface est vérifiée en
  1440×900 et sur un affichage d'iPhone. Aucune des deux mises en page n'est une version
  réduite de l'autre.
- **Le jeton d'application Freebox n'atteint jamais le navigateur** : il est lu dans un
  fichier (`NEOFREEBOX_APP_TOKEN_FILE`), utilisé côté serveur uniquement, et jamais journalisé.
- **Tout ce qui peut couper le réseau** (Wi-Fi, DHCP, mode pont, redémarrage) demande une
  confirmation avant d'être envoyé à la box.
- **Erreurs** : les envelopper avec du contexte, `fmt.Errorf("contexte: %w", err)`.
- **Configuration** : variables d'environnement `NEOFREEBOX_*` ; une mauvaise valeur fait
  échouer le démarrage. Une nouvelle variable va dans le code (`cmd/neofreebox/main.go`),
  le `Dockerfile` (si elle a une valeur par défaut), le `README.md` et
  `deploy/compose.yml.example`.
- **Tests** : pilotés par tableaux ; l'API Freebox OS est simulée avec `httptest` à partir de
  réponses réelles anonymisées (adresses MAC, IP, noms et numéros de série remplacés).

## Déroulé

La branche par défaut est `main` ; le travail courant vise **`dev`**.

```bash
git switch dev
git switch -c ma-modification
```

Les commits suivent les [Conventional Commits](https://www.conventionalcommits.org), en
anglais, avec un corps qui explique le scénario quand la modification n'est pas évidente :

```
feat(dhcp): static leases
fix(fbx): renew the session on auth_required
chore(deps): bump actions/checkout
chore(ci): add job timeouts
```

Les pull requests visent `dev` et sont fusionnées en **squash** (seule méthode activée).
À la publication d'une version, `dev` est fusionnée en squash dans `main` et le tag est poussé.

## CI

- `ci.yml` : build, tests avec détecteur de courses, golangci-lint, govulncheck et analyse
  Trivy, sur chaque pull request.
- `docker-publish.yml` : sur un push vers `dev`, il lance `ci.yml` puis publie `:dev`
  (amd64). Sur un tag `v*`, il lance `ci.yml`, publie `X.Y.Z` et `latest` (amd64, arm64,
  arm/v7), puis crée la release GitHub. Rien n'est publié si la CI est rouge.

## Avant de pousser

```bash
make check            # build, test-race, lint, vuln : ce que lance la CI
make test             # à la place de test-race, sans chaîne C
```

Si tu as modifié quoi que ce soit sous `.github/workflows/`, lance `actionlint`. Chaque
`uses:` **doit** être épinglé sur un SHA de commit complet : le dépôt l'impose, et une
référence non épinglée refusera simplement de s'exécuter.

## Versions

Versionnage sémantique, jugé sur le contenu du diff :

| Montée | Quand |
| ------ | ----- |
| patch | mises à jour de dépendances, corrections, changements de CI seule |
| mineure | nouvelles fonctions |
| majeure | changements incompatibles de configuration ou de comportement |

Les tags git, titres de release et notes portent le `v` (`v1.2.3`) ; les tags d'image
Docker non (`ghcr.io/neotoxicfr/neofreebox:1.2.3`), car `docker/metadata-action` le retire :
une commande `:v1.2.3` pointe vers un tag qui n'existe pas.

Les notes de release sont écrites à la main et ajoutées à la release que crée le workflow :
des sections par thème (ce qui change pour l'utilisateur) et un bloc `### Docker` avec les
commandes `docker pull`. Aucun contenu autogénéré.
