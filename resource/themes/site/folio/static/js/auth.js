(function () {
  async function loadCaptcha(image) {
    try {
      const response = await fetch('/captcha', { credentials: 'same-origin', cache: 'no-store' });
      if (!response.ok) throw new Error('captcha unavailable');
      const data = await response.json();
      document.getElementById(image.id === 'captchaImage' ? 'captchaId' : image.id.replace('-image', '-id')).value = data.captchaId;
      image.src = data.captchaImage;
    } catch (_) {
      image.alt = image.dataset.errorText || image.alt;
    }
  }

  document.querySelectorAll('.auth-captcha-image').forEach(image => {
    image.setAttribute('role', 'button');
    image.tabIndex = 0;
    image.addEventListener('click', () => loadCaptcha(image));
    image.addEventListener('keydown', event => {
      if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); loadCaptcha(image); }
    });
    loadCaptcha(image);
  });
  document.querySelectorAll('[data-refresh-captcha]').forEach(button => {
    button.addEventListener('click', () => loadCaptcha(document.getElementById(button.dataset.refreshCaptcha)));
  });

  const passkey = document.getElementById('passkey-login');
  if (passkey) passkey.addEventListener('click', async () => {
    try {
      if (!window.PublicKeyCredential?.parseRequestOptionsFromJSON) throw new Error(passkey.dataset.unsupportedWebauthn);
      const email = document.getElementById('loginEmail');
      if (!email.value.trim()) { email.focus(); return; }
      const send = async (url, body) => {
        const response = await fetch(url, { method: 'POST', credentials: 'same-origin',
          headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
        if (!response.ok) throw new Error(await response.text());
        return response.json();
      };
      const options = await send('/auth/passkey/login/begin', { email: email.value.trim() });
      const credential = await navigator.credentials.get({ publicKey: PublicKeyCredential.parseRequestOptionsFromJSON(options.publicKey) });
      const result = await send('/auth/passkey/login/finish', { ...credential.toJSON(), return_to: document.querySelector('[name="return_to"]').value });
      if (result.redirect?.startsWith('/') && !result.redirect.startsWith('//')) location.assign(result.redirect);
    } catch (error) { alert(error.message || String(error)); }
  });
})();
