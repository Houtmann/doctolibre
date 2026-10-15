# Doctolibre

Prise de rendez-vous en ligne libre et auto-hébergeable, pensée d'abord pour les
kinésithérapeutes. Un binaire Go + PostgreSQL, rendu serveur (htmx), sans SaaS obligatoire.

Voir [PLAN.md](PLAN.md) pour l'architecture et [AGENTS.md](AGENTS.md) pour les conventions.

```bash
cp .env.example .env
make run     # lance le serveur (à partir de T003)
make check   # fmt + vet + lint + tests
```

Licence : AGPL-3.0.
