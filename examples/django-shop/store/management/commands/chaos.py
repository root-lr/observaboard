from django.core.management.base import BaseCommand, CommandError

from store import chaos


class Command(BaseCommand):
    help = "Simule une panne pour s'entraîner au diagnostic : errors, latency, dependency, tasks, status, reset."

    def add_arguments(self, parser):
        parser.add_argument("mode", choices=[*chaos.MODES, "status", "reset"])
        parser.add_argument("--minutes", type=int, default=10, help="durée de la panne (défaut : 10 minutes)")

    def handle(self, *args, mode, minutes, **options):
        if mode == "reset":
            chaos.reset()
            self.stdout.write("Toutes les pannes simulées sont arrêtées.")
            return
        if mode == "status":
            state = chaos.status()
            if not state:
                self.stdout.write("Aucune panne en cours.")
            for name, left in state.items():
                self.stdout.write(f"{name} : encore {left // 60} min {left % 60} s — {chaos.MODES[name]}")
            return
        if not 1 <= minutes <= 120:
            raise CommandError("--minutes entre 1 et 120")
        chaos.enable(mode, minutes)
        self.stdout.write(f"Panne « {mode} » activée pour {minutes} min : {chaos.MODES[mode]}.")
        self.stdout.write("Observez les dashboards ; l'alerte correspondante se déclenche après quelques minutes.")
