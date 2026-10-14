'use client'

import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { authApi, Role, UserAccount } from '@/lib/api'
import { useAuth } from '@/lib/auth-context'
import { formatDate } from '@/lib/utils'
import { UserPlus, Trash2, KeyRound, X } from 'lucide-react'
import { toast } from 'react-hot-toast'

interface AddUserFormData {
  username: string
  password: string
  role: Role
}

interface ResetPasswordFormData {
  password: string
}

// Rendered only when the logged-in user is an admin (see settings/page.tsx) --
// the backend independently 403s every one of these calls for a viewer, so
// this is purely about not showing controls that can't work, not the actual
// access control.
export default function UserManagement() {
  const { user: currentUser } = useAuth()
  const [users, setUsers] = useState<UserAccount[]>([])
  const [loading, setLoading] = useState(true)
  const [resetTarget, setResetTarget] = useState<UserAccount | null>(null)

  const loadUsers = async () => {
    try {
      setUsers(await authApi.listUsers())
    } catch (error) {
      console.error('Failed to load user accounts:', error)
      toast.error('Failed to load user accounts')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadUsers()
  }, [])

  const {
    register: registerAdd,
    handleSubmit: handleSubmitAdd,
    reset: resetAddForm,
    formState: { errors: addErrors },
  } = useForm<AddUserFormData>({
    defaultValues: { username: '', password: '', role: 'viewer' },
  })

  const [addingUser, setAddingUser] = useState(false)

  const onAddUser = async (data: AddUserFormData) => {
    setAddingUser(true)
    try {
      await authApi.createUser(data)
      toast.success(`Account "${data.username}" created`)
      resetAddForm()
      loadUsers()
    } catch (error: any) {
      toast.error(error.response?.data?.error || 'Failed to create account')
    } finally {
      setAddingUser(false)
    }
  }

  const handleRoleChange = async (targetUser: UserAccount, role: Role) => {
    if (role === targetUser.role) return
    try {
      await authApi.updateUser(targetUser.id, { role })
      toast.success(`${targetUser.username} is now ${role}`)
      loadUsers()
    } catch (error: any) {
      // Most likely cause: this is the last remaining admin -- the backend
      // refuses to demote it so the instance can't lock everyone out.
      toast.error(error.response?.data?.error || 'Failed to change role')
      loadUsers()
    }
  }

  const handleDelete = async (targetUser: UserAccount) => {
    const prompt =
      targetUser.auth_source === 'oidc'
        ? `Delete SSO account "${targetUser.username}"? This signs them out. If they're still in an allowed group at the identity provider, the account is recreated at their next sign-in -- remove them from the group there to block access.`
        : `Delete account "${targetUser.username}"? This can't be undone.`
    if (!confirm(prompt)) {
      return
    }
    try {
      await authApi.deleteUser(targetUser.id)
      toast.success(`Account "${targetUser.username}" deleted`)
      loadUsers()
    } catch (error: any) {
      toast.error(error.response?.data?.error || 'Failed to delete account')
    }
  }

  const {
    register: registerReset,
    handleSubmit: handleSubmitReset,
    reset: resetResetForm,
    formState: { errors: resetErrors },
  } = useForm<ResetPasswordFormData>({ defaultValues: { password: '' } })

  const [resettingPassword, setResettingPassword] = useState(false)

  const closeResetModal = () => {
    setResetTarget(null)
    resetResetForm()
  }

  const onResetPassword = async (data: ResetPasswordFormData) => {
    if (!resetTarget) return
    setResettingPassword(true)
    try {
      await authApi.updateUser(resetTarget.id, { password: data.password })
      toast.success(`Password changed for "${resetTarget.username}" -- any active session for that account was signed out`)
      closeResetModal()
    } catch (error: any) {
      toast.error(error.response?.data?.error || 'Failed to change password')
    } finally {
      setResettingPassword(false)
    }
  }

  return (
    <div className="bg-white rounded-lg shadow">
      <div className="px-6 py-4 border-b border-gray-200">
        <h2 className="text-lg font-medium text-gray-900">User Accounts</h2>
        <p className="mt-1 text-sm text-gray-500">
          Admins can do everything below; viewers can view certificates and CA settings but
          can&apos;t issue, sign, renew, revoke, or manage accounts. Accounts marked SSO sign in
          through the identity provider: they&apos;re created at first sign-in and their role
          follows their identity-provider groups, so it can&apos;t be changed here.
        </p>
      </div>

      <div className="overflow-x-auto">
        <table className="min-w-full divide-y divide-gray-200">
          <thead className="bg-gray-50">
            <tr>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                Username
              </th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                Role
              </th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                Created
              </th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                Actions
              </th>
            </tr>
          </thead>
          <tbody className="bg-white divide-y divide-gray-200">
            {loading ? (
              <tr>
                <td colSpan={4} className="px-6 py-4 text-sm text-gray-500">
                  Loading…
                </td>
              </tr>
            ) : (
              users.map((u) => {
                // AuthUser (from /api/auth/me) only carries username/role, not
                // an id -- usernames are unique, so this is an equally
                // reliable way to spot "this row is me".
                const isSelf = u.username === currentUser?.username
                const isSso = u.auth_source === 'oidc'
                return (
                  <tr key={u.id} className="hover:bg-gray-50">
                    <td className="px-6 py-4 whitespace-nowrap text-sm font-medium text-gray-900">
                      {u.username}
                      {isSso && (
                        <span className="ml-2 inline-flex items-center rounded px-1.5 py-0.5 text-xs font-medium bg-blue-50 text-blue-700">
                          SSO
                        </span>
                      )}
                      {isSelf && <span className="ml-2 text-xs text-gray-400">(you)</span>}
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap">
                      <select
                        value={u.role}
                        onChange={(e) => handleRoleChange(u, e.target.value as Role)}
                        disabled={isSso}
                        title={isSso ? 'Set by identity-provider group membership at each sign-in' : undefined}
                        className="text-sm border-gray-300 rounded-md shadow-sm focus:ring-blue-500 focus:border-blue-500 disabled:bg-gray-50 disabled:text-gray-500 disabled:cursor-not-allowed"
                      >
                        <option value="admin">admin</option>
                        <option value="viewer">viewer</option>
                      </select>
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
                      {formatDate(u.created_at)}
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap text-sm font-medium">
                      <div className="flex space-x-3">
                        <button
                          onClick={() => setResetTarget(u)}
                          disabled={isSso}
                          className="text-blue-600 hover:text-blue-900 disabled:text-gray-300 disabled:cursor-not-allowed"
                          title={isSso ? 'SSO accounts have no step-ui password' : 'Change password'}
                        >
                          <KeyRound className="h-4 w-4" />
                        </button>
                        <button
                          onClick={() => handleDelete(u)}
                          disabled={isSelf}
                          className="text-red-600 hover:text-red-900 disabled:text-gray-300 disabled:cursor-not-allowed"
                          title={isSelf ? "You can't delete your own account while logged in as it" : 'Delete account'}
                        >
                          <Trash2 className="h-4 w-4" />
                        </button>
                      </div>
                    </td>
                  </tr>
                )
              })
            )}
          </tbody>
        </table>
      </div>

      {/* Add User */}
      <div className="px-6 py-4 border-t border-gray-200">
        <h3 className="text-sm font-medium text-gray-900 mb-3">Add User</h3>
        <form onSubmit={handleSubmitAdd(onAddUser)} className="grid grid-cols-1 sm:grid-cols-4 gap-4 items-end">
          <div>
            <label htmlFor="new-username" className="block text-sm font-medium text-gray-700">
              Username
            </label>
            <input
              {...registerAdd('username', { required: 'Username is required' })}
              id="new-username"
              type="text"
              className="mt-1 block w-full border-gray-300 rounded-md shadow-sm focus:ring-blue-500 focus:border-blue-500 sm:text-sm"
            />
            {/* Always rendered, error or not -- a non-breaking space holds
                this line's height so the row below (Password, in
                particular) never grows or shrinks depending on which
                field currently has a validation error. Pressing Enter
                after just the username (a plain browser default -- any
                single text input submits its form on Enter) used to make
                this exact swap happen, visibly shoving Password out of
                line with everything else. */}
            <p className="mt-1 text-sm text-red-600 min-h-[1.25rem]">
              {addErrors.username?.message || ' '}
            </p>
          </div>
          <div>
            <label htmlFor="new-password" className="block text-sm font-medium text-gray-700">
              Password
            </label>
            <input
              {...registerAdd('password', {
                required: 'Password is required',
                minLength: { value: 8, message: 'At least 8 characters' },
              })}
              id="new-password"
              type="password"
              autoComplete="new-password"
              className="mt-1 block w-full border-gray-300 rounded-md shadow-sm focus:ring-blue-500 focus:border-blue-500 sm:text-sm"
            />
            <p className="mt-1 text-sm text-red-600 min-h-[1.25rem]">
              {addErrors.password?.message || ' '}
            </p>
          </div>
          <div>
            <label htmlFor="new-role" className="block text-sm font-medium text-gray-700">
              Role
            </label>
            <select
              {...registerAdd('role')}
              id="new-role"
              className="mt-1 block w-full border-gray-300 rounded-md shadow-sm focus:ring-blue-500 focus:border-blue-500 sm:text-sm"
            >
              <option value="viewer">viewer</option>
              <option value="admin">admin</option>
            </select>
            {/* No validation error ever lands here -- this is purely to
                match the reserved-height line in the other three cells,
                so this row's height doesn't depend on which field errors. */}
            <p className="mt-1 text-sm min-h-[1.25rem]">{' '}</p>
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 invisible">Add</label>
            <button
              type="submit"
              disabled={addingUser}
              className="w-full inline-flex items-center justify-center px-4 py-2 border border-transparent rounded-md shadow-sm text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <UserPlus className="h-4 w-4 mr-2" />
              {addingUser ? 'Adding…' : 'Add User'}
            </button>
            <p className="mt-1 text-sm min-h-[1.25rem]">{' '}</p>
          </div>
        </form>
      </div>

      {/* Change Password Modal */}
      {resetTarget && (
        <div className="fixed inset-0 bg-gray-600 bg-opacity-50 overflow-y-auto h-full w-full z-50">
          <div className="relative top-20 mx-auto p-5 border w-96 shadow-lg rounded-md bg-white">
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-lg font-medium text-gray-900">
                Change Password — {resetTarget.username}
              </h3>
              <button onClick={closeResetModal} className="text-gray-400 hover:text-gray-600">
                <X className="h-6 w-6" />
              </button>
            </div>
            <form onSubmit={handleSubmitReset(onResetPassword)} className="space-y-4">
              <div>
                <label htmlFor="reset-password" className="block text-sm font-medium text-gray-700">
                  New Password
                </label>
                <input
                  {...registerReset('password', {
                    required: 'Password is required',
                    minLength: { value: 8, message: 'At least 8 characters' },
                  })}
                  id="reset-password"
                  type="password"
                  autoComplete="new-password"
                  autoFocus
                  className="mt-1 block w-full border-gray-300 rounded-md shadow-sm focus:ring-blue-500 focus:border-blue-500 sm:text-sm"
                />
                {resetErrors.password && (
                  <p className="mt-1 text-sm text-red-600">{resetErrors.password.message}</p>
                )}
                <p className="mt-2 text-sm text-gray-500">
                  This immediately signs out any device currently logged in as {resetTarget.username}.
                </p>
              </div>
              <div className="flex justify-end space-x-3">
                <button
                  type="button"
                  onClick={closeResetModal}
                  className="px-4 py-2 border border-gray-300 rounded-md shadow-sm text-sm font-medium text-gray-700 bg-white hover:bg-gray-50"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={resettingPassword}
                  className="px-4 py-2 border border-transparent rounded-md shadow-sm text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  {resettingPassword ? 'Saving…' : 'Save'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  )
}
