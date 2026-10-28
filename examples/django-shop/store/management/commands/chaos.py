from django.core.management.base import BaseCommand, CommandError

from store import chaos


class Command(BaseCommand):
    help = "Simulates a failure to practice troubleshooting: errors, latency, dependency, tasks, status, reset."

    def add_arguments(self, parser):
        parser.add_argument("mode", choices=[*chaos.MODES, "status", "reset"])
        parser.add_argument("--minutes", type=int, default=10, help="failure duration (default: 10 minutes)")

    def handle(self, *args, mode, minutes, **options):
        if mode == "reset":
            chaos.reset()
            self.stdout.write("All simulated failures stopped.")
            return
        if mode == "status":
            state = chaos.status()
            if not state:
                self.stdout.write("No active failure.")
            for name, left in state.items():
                self.stdout.write(f"{name}: {left // 60} min {left % 60} s left — {chaos.MODES[name]}")
            return
        if not 1 <= minutes <= 120:
            raise CommandError("--minutes must be between 1 and 120")
        chaos.enable(mode, minutes)
        self.stdout.write(f"Failure \"{mode}\" enabled for {minutes} min: {chaos.MODES[mode]}.")
        self.stdout.write("Watch the dashboards; the matching alert fires after a few minutes.")
