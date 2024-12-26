'use strict';

const handleKey = (e) => {
  if (e.target.matches('input, button, textarea')) {
    return;
  }
  if (e.key === 'S') {
    window.location.href = '/samples';
  }
  if (e.key === 'E') {
    window.location.href = '/login/settings';
  }
  console.log(e.key);
}

document.addEventListener('keyup', handleKey);
