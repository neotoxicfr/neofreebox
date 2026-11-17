## Quoi et pourquoi

<!-- Ce qui change et le problème que ça règle. Lie une issue s'il y en a une. -->

## Vérifications

- [ ] `make build` et `make lint` passent
- [ ] `make test-race` passe (ou `make test` avec une note si tu n'as pas de chaîne C)
- [ ] `make vuln` est propre
- [ ] **Interface touchée ?** vérifiée en 1440×900 **et** sur un affichage de téléphone
- [ ] `actionlint` est propre *(seulement si `.github/workflows/` change : chaque `uses:` épinglé sur un SHA complet)*
- [ ] Aucun secret, jeton ou donnée personnelle dans le diff

## Impact

- [ ] **Nouvelle dépendance Go ?** justifiée ci-dessous
- [ ] **Nouvelle option de configuration ?** ajoutée à `cmd/neofreebox/main.go`, au `README.md` **et** à `deploy/compose.yml.example`

## Notes

<!-- Ce que le relecteur doit savoir : compromis, suites prévues, choses laissées de côté. -->
