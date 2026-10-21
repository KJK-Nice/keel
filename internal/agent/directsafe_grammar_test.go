package agent

import (
	"strings"
	"testing"
)

// The exact grammars of a direct type (§6 rule 13): each allowed form
// passes, and nothing else does.
func TestDirectSafeAllowedForms(t *testing.T) {
	for _, c := range []string{
		`pwd`, `whoami`, `id`, `date`, `uptime`, `hostname`, `uname -a`, `df -h`, `free -h`, `ps aux`,
		`ls`, `ls -l`, `ls -la`, `ls -lah`,
		`ls src`, `ls -la /var/log`, `ls -lah build/out`, `ls /etc/nginx/`, `ls -l a_b-c.d`,
		`docker ps`, `docker ps -a`,
		`kubectl get pods`, `kubectl get nodes`, `kubectl get svc`, `kubectl get services`, `kubectl get deployments`,
		`kubectl get ns`, `kubectl get namespaces`, `kubectl get ingress`, `kubectl get pvc`, `kubectl get events`,
		`kubectl get jobs`, `kubectl get cronjobs`, `kubectl get statefulsets`, `kubectl get daemonsets`, `kubectl get replicasets`,
		`kubectl get pods -n kube-system`, `kubectl get pods -A`, `kubectl get pods -o wide`,
		`kubectl get pods -n web-1 -o wide`, `kubectl get pods -o wide -A`,
		`systemctl status nginx --no-pager`, `systemctl status getty@tty1.service --no-pager`,
	} {
		if !DirectSafe(c) {
			t.Errorf("DirectSafe(%q) = false, want a direct type", c)
		}
	}
}

func TestDirectSafeRefusesOutsideTheGrammar(t *testing.T) {
	for _, c := range []string{
		// reads the old allowlist took, now outside the grammar
		`ls -lh --color=auto`, `id -u`, `hostname -f`, `date +%F`, `du -sh /var/log`, `cat /etc/hosts`,
		`head -n 20 f`, `tail -n 50 f`, `grep -n x f`, `ps aux | grep nginx`, `ps -ef`, `ss -ltnp`,
		`echo keel-agent-ok`, `wc -l f`, `systemctl --no-pager status nginx`, `journalctl --no-pager -u x`,
		`docker logs web`, `kubectl logs web`, `ip -br a`, `lsblk`, `ping -c 3 example.com`, `which kubectl`,
		`printenv HOME`, `uname`, `df`, `ps`, `ls -R`, `ls -R src`, `ls -a`, `ls src lib`, `ls -la -l`,
		`kubectl get secrets`, `kubectl get configmaps`, `kubectl get pods -o yaml`, `kubectl get pods -o json`,
		`kubectl get pods -n kube-system -A`, `kubectl get pods -n KUBE`, `kubectl get pods -n`, `kubectl get pods -o`,
		`kubectl describe pod x`, `kubectl get pods x`, `kubectl -n x get pods`, `kubectl get pods --context x`,
		`systemctl status nginx`, `systemctl status --no-pager nginx`, `systemctl status -x --no-pager`,
		`systemctl show nginx --no-pager`, `systemctl cat nginx --no-pager`, `systemctl status 'nginx' --no-pager`,
		`docker ps -q`, `docker ps  -a`, ` pwd`, `pwd `, `pwd  `,
		// fourth review: optional-value flags, swallowed --no-pager
		`lsof -i -P`, `lsof -i`, `journalctl -n --no-pager`, `systemctl -p --no-pager status x`,
		// macOS ps -C / -E (environment)
		`ps -C`, `ps -E`, `ps auxE`, `ps -Ef`,
		// GNU tail +f
		`tail +f x`,
		// home directories by ., .., //, case, and /var/root
		`ls /home/u/../u`, `ls /home/./u`, `ls //home/u`, `ls /USERS/me`, `ls /Users/me/Documents`, `ls /var/root`,
		`ls /private/var/root`, `ls /root`, `ls /home`, `ls ./x`, `ls ../x`, `ls x/../y`, `ls x/./y`, `ls .`, `ls ..`,
		// secret paths by //, /./ and case
		`ls /etc//shadow`, `ls /etc/./shadow`, `ls /ETC/SHADOW`, `ls .ssh`, `ls x/.SSH`, `ls /x/.aws/`, `ls /proc/1`,
		`ls secrets`, `ls id_rsa.pub`, `ls tls.KEY`, `ls .env`, `ls -la .kube`,
		// APFS case-folding: ſ (U+017F) folds to s; only ASCII is allowed
		"ls .ſsh", "ls ſecrets",
		// named-secret printenv, env printers, unbounded ping
		`printenv ANTHROPIC_API_KEY`, `ping example.com`,
		// paths that start with -, are too long, or hold other characters
		`ls -x`, `ls --all`, `ls a*b`, `ls a?b`, `ls a~b`, `ls a:b`, `ls a@b`,
		"ls " + strings.Repeat("a", 129),
	} {
		if DirectSafe(c) {
			t.Errorf("DirectSafe(%q) = true, want the dialog", c)
		}
	}
}
