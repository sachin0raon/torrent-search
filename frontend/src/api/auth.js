import { request } from './client.js';

export const authApi = {
  me: (signal) => request('/api/auth/me', { signal }),
  login: (username, password) =>
    request('/api/auth/login', { method: 'POST', body: { username, password } }),
  register: (username, password) =>
    request('/api/auth/register', { method: 'POST', body: { username, password } }),
  logout: () => request('/api/auth/logout', { method: 'POST' }),
  changePassword: (currentPassword, newPassword) =>
    request('/api/auth/change-password', {
      method: 'POST',
      body: { current_password: currentPassword, new_password: newPassword },
    }),
  streamToken: (kind, id) =>
    request('/api/auth/stream-token', { method: 'POST', body: { kind, id } }),
};

export const adminApi = {
  listUsers: (signal) => request('/api/admin/users', { signal }),
  approve: (id) => request(`/api/admin/users/${id}/approve`, { method: 'POST' }),
  reject: (id) => request(`/api/admin/users/${id}/reject`, { method: 'POST' }),
  deactivate: (id) => request(`/api/admin/users/${id}/deactivate`, { method: 'POST' }),
  reactivate: (id) => request(`/api/admin/users/${id}/reactivate`, { method: 'POST' }),
  resetPassword: (id, newPassword) =>
    request(`/api/admin/users/${id}/password`, {
      method: 'POST',
      body: { new_password: newPassword },
    }),
  remove: (id) => request(`/api/admin/users/${id}`, { method: 'DELETE' }),
};
