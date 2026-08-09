    const form = document.getElementById('login-form');
    const submit = document.getElementById('submit');
    const errorEl = document.getElementById('error');
    const resultEl = document.getElementById('result');
    const tokenEl = document.getElementById('token');
    const copyBtn = document.getElementById('copy');

    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      errorEl.classList.add('hidden');
      resultEl.classList.add('hidden');
      submit.disabled = true;
      submit.textContent = 'Signing in…';
      try {
        const ttlValue = document.getElementById('ttl').value;
        const payload = {
          username: document.getElementById('username').value,
          password: document.getElementById('password').value,
        };
        if (ttlValue) {
          payload.ttl_seconds = Number(ttlValue);
        }
        const res = await fetch('/login', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
        if (!res.ok) {
          throw new Error(res.status === 401 ? 'Invalid credentials' : 'Login failed');
        }
        const data = await res.json();
        tokenEl.value = data.token;
        resultEl.classList.remove('hidden');
      } catch (err) {
        errorEl.textContent = err.message;
        errorEl.classList.remove('hidden');
      } finally {
        submit.disabled = false;
        submit.textContent = 'Sign in';
      }
    });

    copyBtn.addEventListener('click', async () => {
      await navigator.clipboard.writeText(tokenEl.value);
      copyBtn.textContent = 'Copied!';
      setTimeout(() => { copyBtn.textContent = 'Copy to clipboard'; }, 1500);
    });

    fetch('/version').then((res) => res.json()).then((info) => {
      document.getElementById('build-info').textContent =
        ' · ' + (info.version ? info.version + ' · ' : '') + info.commit;
    }).catch(() => {});
