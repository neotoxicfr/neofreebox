# Politique de sécurité

## Versions prises en charge

Seule la dernière version est prise en charge. Les correctifs arrivent sur `dev` et
partent dans le prochain tag ; il n'y a pas de rétroportage.

| Version | Prise en charge |
| ------- | --------------- |
| dernière version | ✅ |
| `dev` | ⚠️ préversion, sans garantie |
| toute version plus ancienne | ❌ |

## Signaler une vulnérabilité

Ouvre un [avis de sécurité privé](https://github.com/neotoxicfr/neofreebox/security/advisories/new)
plutôt qu'une issue publique. Indique la version, la configuration concernée et un
moyen de reproduire le problème.

## Modèle de sécurité

neofreebox détient un jeton d'application Freebox OS avec tous les droits sur la box. Il
est donc conçu pour se placer derrière un reverse proxy qui authentifie (Authelia,
Authentik…) et possède aussi sa propre connexion :

- Un seul compte, créé au premier lancement avec un code d'installation à usage unique
  affiché uniquement dans le journal du conteneur : atteindre la page ne suffit pas. Le mot
  de passe est haché avec bcrypt dans `account.json`, dans le dossier de données. Les
  connexions échouées sont limitées à 10 par 15 minutes pour toute l'application (elle est
  derrière un proxy, les IP des clients n'ont donc pas de sens). Quelqu'un qui franchirait
  le proxy pourrait s'en servir pour te bloquer 15 minutes : compromis assumé.
- Les sessions sont des cookies `HttpOnly`, `Secure`, `SameSite=Lax`, gardées en mémoire :
  un redémarrage te déconnecte.
- Toute requête qui modifie quelque chose est un POST vérifié par la protection
  cross-origin de Go.
- L'API JSON et le serveur MCP exigent un jeton porteur `nfb_…` créé depuis la page Compte.
  Seule son empreinte SHA-256 est conservée (`tokens.json`) ; il n'est affiché qu'une fois et
  se révoque à tout moment. Les opérations dangereuses exigent `"confirm": true`.
- Le jeton d'application est lu dans un fichier (`NEOFREEBOX_APP_TOKEN_FILE`), jamais envoyé
  au navigateur ni écrit dans les journaux. Traite ce fichier comme un secret : il donne le
  contrôle complet de la box.
- La box est jointe en HTTPS, avec les certificats racine de Free embarqués.
- `NEOFREEBOX_READ_ONLY=1` refuse toute écriture vers la box, pour une démonstration ou un essai.

## Durcissement du conteneur

L'image est `distroless/static` (certificats et fuseaux horaires, ni shell ni gestionnaire de
paquets) et s'exécute sans droits root. Le compose d'exemple ajoute `read_only: true`,
`cap_drop: [ALL]` et `no-new-privileges:true`.

## Chaîne d'approvisionnement

- Chaque GitHub Action est épinglée sur un SHA de commit complet, et le dépôt *impose* cet
  épinglage : une étiquette détournée ne peut pas modifier un workflow.
- La CI lance `govulncheck` et une analyse Trivy du système de fichiers (vulnérabilités,
  secrets, mauvaises configurations) à chaque push sur `dev`, chaque pull request et chaque
  tag de version, et échoue sur toute faille `CRITICAL`/`HIGH` corrigeable. Aucune image n'est
  publiée sans ce passage.
- Les alertes Dependabot et les mises à jour de sécurité sont activées.
